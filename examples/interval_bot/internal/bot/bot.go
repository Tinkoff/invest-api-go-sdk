package bot

import (
	"context"
	"errors"
	"fmt"
	"github.com/montanaflynn/stats"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"math"
	"sort"
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
	// IntervalUpdateDelay - Время ожидания для перерасчета интервала цены
	IntervalUpdateDelay time.Duration
	// TopInstrumentsQuantity - Топ лучших инструментов по волатильности
	TopInstrumentsQuantity int
	// SellOut - Если true, то по достижению дедлайна бот выходит из всех активных позиций
	SellOut bool
}

type interval struct {
	high, low float64
}

type Bot struct {
	StrategyConfig IntervalStrategyConfig
	Client         *investgo.Client

	ctx       context.Context
	cancelBot context.CancelFunc

	executor *Executor
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
	}, nil
}

func (b *Bot) Run() error {
	wg := &sync.WaitGroup{}

	// проверяем баланс денежных средств на счете
	err := b.checkMoneyBalance("RUB", 200000)
	if err != nil {
		b.Client.Logger.Fatalf(err.Error())
	}
	// создаем хранилище исторических свечей
	marketDataService := b.Client.NewMarketDataServiceClient()
	candles := NewCandlesStorage(marketDataService)

	// загружаем минутные свечи по всем инструментам для анализа волатильности
	err = candles.LoadCandlesHistory(b.StrategyConfig.Instruments, pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
		time.Now().Add(-1*24*time.Hour), time.Now())
	if err != nil {
		return err
	}

	// отбор топ инструментов по волатильности

	// результаты анализа
	analyseResult := make([]*analyseResponse, 0, len(b.StrategyConfig.Instruments))

	// запуск анализа инструментов по их историческим свечам
	for _, id := range b.StrategyConfig.Instruments {
		tempId := id
		hc, err := candles.Candles(tempId)
		if err != nil {
			return err
		}
		resp, err := b.analyseCandles(tempId, hc)
		if err != nil {
			return err
		}
		analyseResult = append(analyseResult, resp)
	}

	// сортировка по убыванию максимальной волатильности инструментов
	sort.Slice(analyseResult, func(i, j int) bool {
		return analyseResult[i].volatilityMax > analyseResult[j].volatilityMax
	})

	for _, response := range analyseResult {
		fmt.Printf("vol = %v h/l = %.9f %.9f id = %v\n", response.volatilityMax, response.interval.high,
			response.interval.low, response.id)
	}

	// берем первые топ TopInstrumentsQuantity инструментов по волатильности
	topInstrumentsIntervals := make(map[string]interval, b.StrategyConfig.TopInstrumentsQuantity)
	topInstrumentsIds := make([]string, 0, b.StrategyConfig.TopInstrumentsQuantity)

	if b.StrategyConfig.TopInstrumentsQuantity > len(analyseResult) {
		return fmt.Errorf("TopInstrumentsQuantity = %v, but max value = %v\n",
			b.StrategyConfig.TopInstrumentsQuantity, len(analyseResult))
	}

	for i := 0; i < b.StrategyConfig.TopInstrumentsQuantity; i++ {
		r := analyseResult[i]
		topInstrumentsIntervals[r.id] = r.interval
		topInstrumentsIds = append(topInstrumentsIds, r.id)
	}

	// запуск исполнителя, он начнет торговать топовыми инструментами
	err = b.executor.Start(topInstrumentsIntervals)
	if err != nil {
		return err
	}

	// по тикеру обновляем
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		ticker := time.NewTicker(b.StrategyConfig.IntervalUpdateDelay)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := b.UpdateIntervals(candles, topInstrumentsIds)
				if err != nil {
					b.Client.Logger.Errorf(err.Error())
				}
			}
		}
	}(b.ctx)

	// Завершение работы бота по его контексту: вызов Stop() или отмена по дедлайну
	<-b.ctx.Done()
	b.Client.Logger.Infof("stop interval bot...")

	// явно завершаем работу исполнителя, если нужно выходим из всех позиций
	err = b.executor.Stop(b.StrategyConfig.SellOut)
	if err != nil {
		return err
	}

	wg.Wait()
	return nil
}

func (b *Bot) Stop() {
	b.cancelBot()
}

func (b *Bot) UpdateIntervals(storage *CandlesStorage, ids []string) error {
	for _, id := range ids {
		// обновляем историю по инструменту
		err := storage.UpdateCandlesHistory(id)
		if err != nil {
			return err
		}
		candles, err := storage.Candles(id)
		if err != nil {
			return err
		}
		// пересчитываем интервал
		resp, err := b.analyseCandles(id, candles)
		if err != nil {
			return err
		}
		// вызываем исполнителя
		err = b.executor.UpdateInterval(resp.id, resp.interval)
		if err != nil {
			return err
		}
	}
	return nil
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

type analyseResponse struct {
	id            string
	interval      interval
	volatilityMax float64
}

// analyseCandles - Расчет максимальной волатильности и интервала цены для инструмента
func (b *Bot) analyseCandles(id string, candles []*pb.HistoricCandle) (*analyseResponse, error) {
	// получили список средних цен
	midPrices := make([]float64, 0, len(candles))
	for _, candle := range candles {
		midPrices = append(midPrices, midPrice(candle))
	}
	// ищем моду, те находим цену с макс пересечениями
	modes, err := stats.Mode(midPrices)
	var mode float64
	if err != nil {
		return nil, err
	}
	if len(modes) > 0 {
		mode = modes[0]
	}
	instr, ok := b.executor.instruments[id]
	if !ok {
		return nil, fmt.Errorf("%v min price increment not found", id)
	}
	// maxVol - Кол-во пересечений с модой
	i, maxVol := b.findInterval(mode, instr.minPriceInc, candles)
	return &analyseResponse{
		id:            id,
		interval:      i,
		volatilityMax: maxVol,
	}, nil
}

// analyseCandlesByMathStat - Расчет максимальной волатильности и интервала цены для инструмента
func (b *Bot) analyseCandlesByMathStat(id string, candles []*pb.HistoricCandle) (*analyseResponse, error) {
	// получили список средних цен
	midPrices := make([]float64, 0, len(candles))
	for _, candle := range candles {
		midPrices = append(midPrices, midPrice(candle))
	}
	median, err := stats.Median(midPrices)
	if err != nil {
		return nil, err
	}
	low, err := stats.Percentile(midPrices, 10)
	if err != nil {
		return nil, err
	}
	high, err := stats.Percentile(midPrices, 90)
	if err != nil {
		return nil, err
	}
	i := interval{
		high: high,
		low:  low,
	}
	instr, ok := b.executor.instruments[id]
	if !ok {
		return nil, fmt.Errorf("%v min price increment not found", id)
	}
	// maxVol - Кол-во пересечений с медианой
	maxVol := crosses(floatToQuotation(median, instr.minPriceInc).ToFloat(), candles)
	return &analyseResponse{
		id:            id,
		interval:      i,
		volatilityMax: float64(maxVol),
	}, nil
}

// midPrice - Средняя цена свечи
func midPrice(c *pb.HistoricCandle) float64 {
	return (c.GetHigh().ToFloat() + c.GetLow().ToFloat() + c.GetClose().ToFloat() + c.GetOpen().ToFloat()) / 4
}

// findInterval - Поиск интервала
func (b *Bot) findInterval(mode float64, inc *pb.Quotation, candles []*pb.HistoricCandle) (interval, float64) {
	// минимальный профит в валюте / шаг цены = начальное кол-во шагов цены в интервале
	k := int(math.Round((mode * b.StrategyConfig.MinProfit / 100) / inc.ToFloat()))
	mode = floatToQuotation(mode, inc).ToFloat()

	upper, lower := mode, mode
	var maxCrosses int64
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

	// volatility = maxCrosses * (width/median * 100)
	var initialVolatility, volatility, delta float64
	maxCrosses = intervalCrosses(upper, lower, candles)
	initialVolatility = ((upper - lower) / mode * 100) * float64(maxCrosses)
	volatility = initialVolatility

	for {
		u, l := upper, lower
		mc := maxCrosses
		upper = upper + inc.ToFloat()
		lower = lower - inc.ToFloat()

		upperCrosses := crosses(upper, candles)
		lowerCrosses := crosses(lower, candles)

		if upperCrosses > lowerCrosses {
			lower = lower + inc.ToFloat()
		} else {
			upper = upper - inc.ToFloat()
		}
		maxCrosses = intervalCrosses(upper, lower, candles)
		tempVolatility := ((upper - lower) / mode * 100) * float64(maxCrosses)

		delta = tempVolatility - volatility
		volatility = tempVolatility
		if delta < 0 {
			upper, lower = u, l
			maxCrosses = mc
			volatility = ((upper - lower) / mode * 100) * float64(maxCrosses)
			break
		}
	}
	return interval{
		high: upper,
		low:  lower,
	}, volatility
}

func intervalCrosses(h, l float64, candles []*pb.HistoricCandle) (count int64) {
	for _, candle := range candles {
		if h <= candle.GetHigh().ToFloat() && l >= candle.GetLow().ToFloat() {
			count++
		}
	}
	return count
}

// crosses - Количество пересечений свечей с горизонтальной линией цены price
func crosses(price float64, candles []*pb.HistoricCandle) int64 {
	var count int64
	for _, candle := range candles {
		if price <= candle.GetHigh().ToFloat() && price >= candle.GetLow().ToFloat() {
			count++
		}
	}
	return count
}

// timeIntervalByDays - Функция возвращает ближайший временной интервал до now, в котором содержится reqDays рабочих дней
func timeIntervalByDays(reqDays int, now time.Time) (from time.Time, to time.Time) {
	y, m, d := now.Date()
	daysFromMonday := int(now.Weekday() - time.Monday)
	switch {
	// если сегодня пн
	case daysFromMonday == 0:
		// запрашиваем за вт-пт той недели
		to = time.Date(y, m, d, 0, 0, 0, 0, time.Local).Add(-48 * time.Hour)
		from = to.Add(-1 * time.Duration(reqDays) * 24 * time.Hour)
	// если сегодня вт-чт
	case daysFromMonday > 0 && daysFromMonday < 4:
		delta := time.Duration(reqDays - daysFromMonday)
		// от сегодня до пн
		to = time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		// from1 - это понедельник текущей недели
		from1 := to.Add(-1 * 24 * time.Hour * time.Duration(daysFromMonday))
		// остаток с той недели
		to2 := from1.Add(-1 * 24 * time.Hour * 2)
		from = to2.Add(-1 * 24 * time.Hour * delta)
	// сегодня пт-сб
	case daysFromMonday >= 4:
		to = time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		from = to.Add(-1 * time.Duration(reqDays) * 24 * time.Hour)
	//  сегодня вс
	case daysFromMonday == -1:
		to = time.Date(y, m, d-1, 0, 0, 0, 0, time.Local)
		from = to.Add(-1 * time.Duration(reqDays) * 24 * time.Hour)
	}
	return from, to
}

func (b *Bot) BackTest() (float64, error) {
	// качаем свечи за 3 дня

	// создаем хранилище исторических свечей
	marketDataService := b.Client.NewMarketDataServiceClient()
	candles := NewCandlesStorage(marketDataService)

	// загружаем минутные свечи по всем инструментам для анализа волатильности
	from, to := timeIntervalByDays(3, time.Now().Add(-24*time.Hour))
	err := candles.LoadCandlesHistory(b.StrategyConfig.Instruments, pb.CandleInterval_CANDLE_INTERVAL_1_MIN, from, to)
	if err != nil {
		return 0, err
	}

	// считаем на трех дня

	// отбор топ инструментов по волатильности

	// результаты анализа
	analyseResult := make([]*analyseResponse, 0, len(b.StrategyConfig.Instruments))

	// запуск анализа инструментов по их историческим свечам
	for _, id := range b.StrategyConfig.Instruments {
		tempId := id
		hc, err := candles.Candles(tempId)
		if err != nil {
			return 0, err
		}
		resp, err := b.analyseCandlesByMathStat(tempId, hc)
		if err != nil {
			return 0, err
		}
		analyseResult = append(analyseResult, resp)
	}

	// сортировка по убыванию максимальной волатильности инструментов
	sort.Slice(analyseResult, func(i, j int) bool {
		return analyseResult[i].volatilityMax > analyseResult[j].volatilityMax
	})

	for _, response := range analyseResult {
		fmt.Printf("vol = %v h/l = %.9f %.9f id = %v\n", response.volatilityMax, response.interval.high,
			response.interval.low, response.id)
	}

	// берем первые топ TopInstrumentsQuantity инструментов по волатильности
	topInstrumentsIntervals := make(map[string]interval, b.StrategyConfig.TopInstrumentsQuantity)
	topInstrumentsIds := make([]string, 0, b.StrategyConfig.TopInstrumentsQuantity)

	if b.StrategyConfig.TopInstrumentsQuantity > len(analyseResult) {
		return 0, fmt.Errorf("TopInstrumentsQuantity = %v, but max value = %v\n",
			b.StrategyConfig.TopInstrumentsQuantity, len(analyseResult))
	}

	for i := 0; i < b.StrategyConfig.TopInstrumentsQuantity; i++ {
		r := analyseResult[i]
		topInstrumentsIntervals[r.id] = r.interval
		topInstrumentsIds = append(topInstrumentsIds, r.id)
	}
	// проверяем на 4ом
	from, to = timeIntervalByDays(1, time.Now())
	yesterdayCandlesStorage := NewCandlesStorage(marketDataService)
	err = yesterdayCandlesStorage.LoadCandlesHistory(topInstrumentsIds, pb.CandleInterval_CANDLE_INTERVAL_1_MIN, from, to)
	if err != nil {
		return 0, err
	}

	var requiredMoneyForStart float64

	for _, id := range topInstrumentsIds {
		currentInterval := topInstrumentsIntervals[id]
		lot := b.executor.instruments[id].lot

		requiredMoneyForStart += currentInterval.low * float64(lot)
	}

	var totalProfit float64

	for _, id := range topInstrumentsIds {
		yesterdayCandles, err := yesterdayCandlesStorage.Candles(id)
		if err != nil {
			return 0, err
		}
		inStock := false
		currentInterval := topInstrumentsIntervals[id]
		delta := currentInterval.high - currentInterval.low
		lot := b.executor.instruments[id].lot
		for _, candle := range yesterdayCandles {
			if inStock {
				if currentInterval.high <= candle.GetHigh().ToFloat() {
					// могли бы продать
					totalProfit += delta * float64(lot)
					inStock = false
				}
			} else {
				if currentInterval.low >= candle.GetLow().ToFloat() {
					// могли бы купить
					inStock = true
				}
			}
		}
	}

	b.Client.Logger.Infof("profit in percent = %.9f", (totalProfit/requiredMoneyForStart)*100)

	return totalProfit, nil
}
