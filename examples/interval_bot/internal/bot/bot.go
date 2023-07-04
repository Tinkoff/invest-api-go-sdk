package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/montanaflynn/stats"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"math"
	"strings"
	"sync"
	"time"
)

type IntervalStrategyConfig struct {
	// Instruments - слайс идентификаторов инструментов
	Instruments []string
	// Quantity - Начальное кол-во лотов для покупки
	Quantity int64
	// MinProfit - Минимальный процент выгоды, с которым можно совершать сделки
	MinProfit float64
	// SellOut - Если true, то по достижению дедлайна бот выходит из всех активных позиций
	SellOut bool
}

type interval struct {
	high, low *pb.Quotation
}

type Bot struct {
	StrategyConfig IntervalStrategyConfig
	Client         *investgo.Client

	ctx       context.Context
	cancelBot context.CancelFunc

	executor *Executor

	intervals map[string]interval
}

func NewBot(ctx context.Context, client *investgo.Client, config IntervalStrategyConfig) (*Bot, error) {
	botCtx, cancel := context.WithCancel(ctx)

	// по конфигу стратегии заполняем map для executor
	instrumentService := client.NewInstrumentsServiceClient()
	instruments := make(map[string]Instrument, len(config.Instruments))

	for _, instrument := range config.Instruments {
		// в данном случае ключ это uid, поэтому используем LotByUid()
		resp, err := instrumentService.InstrumentByUid(instrument)
		if err != nil {
			cancel()
			return nil, err
		}
		instruments[instrument] = Instrument{
			quantity:    config.Quantity,
			entryPrice:  0,
			lot:         resp.GetInstrument().GetLot(),
			currency:    resp.GetInstrument().GetCurrency(),
			minPriceInc: resp.GetInstrument().GetMinPriceIncrement(),
		}
	}

	return &Bot{
		StrategyConfig: config,
		Client:         client,
		ctx:            botCtx,
		cancelBot:      cancel,
		executor:       NewExecutor(ctx, client, instruments),
		intervals:      make(map[string]interval, len(instruments)),
	}, nil
}

func (b *Bot) Run() error {
	wg := &sync.WaitGroup{}

	err := b.checkMoneyBalance("RUB", 200000)
	if err != nil {
		b.Client.Logger.Fatalf(err.Error())
	}
	candles := make(map[string][]*pb.HistoricCandle, len(b.StrategyConfig.Instruments))

	marketDataService := b.Client.NewMarketDataServiceClient()
	for _, instrument := range b.StrategyConfig.Instruments {
		c, err := marketDataService.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
			Instrument: instrument,
			Interval:   pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
			From:       time.Now().Add(-1 * 96 * time.Hour),
			To:         time.Now(),
			File:       false,
			FileName:   "",
		})
		if err != nil {
			return err
		}
		candles[instrument] = c
	}

	// так как исполнитель тоже слушает стримы, его нужно явно остановить
	b.executor.Stop()

	wg.Wait()
	return nil
}

func (b *Bot) Stop() {
	b.cancelBot()
}

// checkMoneyBalance - проверка доступного баланса денежных средств
func (b *Bot) checkMoneyBalance(currency string, required float64) error {
	operationsService := b.Client.NewOperationsServiceClient()

	resp, err := operationsService.GetPositions(b.Client.Config.AccountId)
	if err != nil {
		return err
	}
	var balance float64
	money := resp.GetMoney()
	for _, m := range money {
		b.Client.Logger.Infof("money balance = %v %v", m.ToFloat(), m.GetCurrency())
		if strings.EqualFold(m.GetCurrency(), currency) {
			balance = m.ToFloat()
		}
	}

	if diff := balance - required; diff < 0 {
		if strings.HasPrefix(b.Client.Config.EndPoint, "sandbox") {
			sandbox := b.Client.NewSandboxServiceClient()
			resp, err := sandbox.SandboxPayIn(&investgo.SandboxPayInRequest{
				AccountId: b.Client.Config.AccountId,
				Currency:  currency,
				Unit:      int64(-diff),
				Nano:      0,
			})
			if err != nil {
				return err
			}
			b.Client.Logger.Infof("sandbox auto pay in, balance = %v", resp.GetBalance().ToFloat())
			err = b.executor.updatePositionsUnary()
			if err != nil {
				return err
			}
		} else {
			return errors.New("not enough money on balance")
		}
	}

	return nil
}

// analyseCandles - Расчет максимальной волатильности и интервала цены для инструмента
func (b *Bot) analyseCandles(id string, candles []*pb.HistoricCandle) (interval, float64, error) {
	// получили список средних цен
	midPrices := make([]float64, 0, len(candles))
	for _, candle := range candles {
		midPrices = append(midPrices, midPrice(candle))
	}
	// ищем моду, те находим цену с макс пересечениями
	data := stats.LoadRawData(midPrices)
	modes, err := stats.Mode(data)
	var mode float64
	if err != nil {
		return interval{}, 0, err
	}
	if len(modes) > 0 {
		mode = modes[0]
	}
	instr, ok := b.executor.instruments[id]
	if !ok {
		return interval{}, 0, fmt.Errorf("%v min price increment not found", id)
	}
	up, low := b.findInterval(mode, instr.minPriceInc, candles)
	maxVol := crosses(floatToQuotation(mode, instr.minPriceInc).ToFloat(), candles)
	return interval{
		high: floatToQuotation(up, instr.minPriceInc),
		low:  floatToQuotation(low, instr.minPriceInc),
	}, float64(maxVol), nil
}

// midPrice - Средняя цена свечи
func midPrice(c *pb.HistoricCandle) float64 {
	return (c.GetHigh().ToFloat() + c.GetLow().ToFloat() + c.GetClose().ToFloat() + c.GetOpen().ToFloat()) / 4
}

// findInterval - Поиск интервала по моде и списку свечей
func (b *Bot) findInterval(mode float64, inc *pb.Quotation, candles []*pb.HistoricCandle) (float64, float64) {
	// минимальный профит в валюте / шаг цены = начальное кол-во шагов цены в интервале
	k := int(math.Round((mode * b.StrategyConfig.MinProfit / 100) / inc.ToFloat()))
	mode = floatToQuotation(mode, inc).ToFloat()

	upper, lower := mode, mode
	for i := 1; i <= k; i++ {
		upper = upper + inc.ToFloat()
		lower = lower - inc.ToFloat()

		upperCrosses := crosses(upper, candles)
		lowerCrosses := crosses(lower, candles)

		if upperCrosses > lowerCrosses {
			lower = lower + inc.ToFloat()
		} else {
			upper = upper - inc.ToFloat()
		}
	}
	// TODO сделать расширение по шагам
	return upper, lower
}

// crosses - Количество пересечений свечей с горизонтальной линией цены price
func crosses(price float64, candles []*pb.HistoricCandle) int64 {
	var count int64
	for _, candle := range candles {
		if price < candle.GetHigh().ToFloat() && price > candle.GetLow().ToFloat() {
			count++
		}
	}
	return count
}
