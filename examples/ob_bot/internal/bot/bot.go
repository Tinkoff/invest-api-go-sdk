package bot

import (
	"context"
	"errors"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"strings"
	"sync"
)

// QUANTITY - Кол-во лотов инструментов, которыми торгует бот
const QUANTITY = 1

// OrderBookStrategyConfig - Конфигурация стратегии на стакане
type OrderBookStrategyConfig struct {
	// Instruments - слайс идентификаторов инструментов
	Instruments []string
	// Depth - Глубина стакана
	Depth int32
	//  Если кол-во бид/аск больше чем BuyRatio - покупаем
	BuyRatio float64
	//  Если кол-во аск/бид больше чем SellRatio - продаем
	SellRatio float64
	// MinProfit - Минимальный процент выгоды, с которым можно совершать сделки
	MinProfit float64
	// SellOut - Если true, то по достижению дедлайна бот выходит из всех активных позиций
	SellOut bool
}

type Bot struct {
	StrategyConfig OrderBookStrategyConfig
	Client         *investgo.Client

	ctx       context.Context
	cancelBot context.CancelFunc

	executor *Executor
}

// NewBot - Создание экземпляра бота на стакане
func NewBot(ctx context.Context, c *investgo.Client, config OrderBookStrategyConfig) (*Bot, error) {
	botCtx, cancelBot := context.WithCancel(ctx)

	// по конфигу стратегии заполняем map для executor
	instrumentService := c.NewInstrumentsServiceClient()
	instruments := make(map[string]Instrument, len(config.Instruments))

	for _, instrument := range config.Instruments {
		// в данном случае ключ это uid, поэтому используем LotByUid()
		resp, err := instrumentService.InstrumentByUid(instrument)
		if err != nil {
			cancelBot()
			return nil, err
		}
		instruments[instrument] = Instrument{
			quantity:   QUANTITY,
			inStock:    false,
			entryPrice: 0,
			lot:        resp.GetInstrument().GetLot(),
			currency:   resp.GetInstrument().GetCurrency(),
		}
	}
	return &Bot{
		Client:         c,
		StrategyConfig: config,
		ctx:            botCtx,
		cancelBot:      cancelBot,
		executor:       NewExecutor(ctx, c, instruments, config.MinProfit),
	}, nil
}

// Run - Запуск бота
func (b *Bot) Run() error {
	wg := &sync.WaitGroup{}

	err := b.checkMoneyBalance("RUB", 200000)
	if err != nil {
		b.Client.Logger.Fatalf(err.Error())
	}

	// инфраструктура для работы стратегии: запрос, получение, преобразование рыночных данных
	MarketDataStreamService := b.Client.NewMarketDataStreamClient()
	stream, err := MarketDataStreamService.MarketDataStream()
	if err != nil {
		return err
	}
	pbOrderBooks, err := stream.SubscribeOrderBook(b.StrategyConfig.Instruments, b.StrategyConfig.Depth)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := stream.Listen()
		if err != nil {
			b.Client.Logger.Errorf(err.Error())
		}
	}()

	orderBooks := make(chan OrderBook)
	defer close(orderBooks)

	// чтение из стрима
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-pbOrderBooks:
				if !ok {
					return
				}
				orderBooks <- transformOrderBook(ob)
			}
		}
	}(b.ctx)

	// данные готовы, далее идет принятие решения и возможное выставление торгового поручения
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		profit, err := b.HandleOrderBooks(ctx, orderBooks)
		if err != nil {
			b.Client.Logger.Errorf(err.Error())
		}
		b.Client.Logger.Infof("profit by strategy = %.9f", profit)
	}(b.ctx)

	// Завершение работы бота по его контексту: вызов Stop() или отмена по дедлайну
	<-b.ctx.Done()
	b.Client.Logger.Infof("stop bot on order book...")

	// стримы работают на контексте клиента, завершать их нужно явно
	stream.Stop()

	// если нужно, то в конце торговой сессии выходим из всех, открытых ботом, позиций
	if b.StrategyConfig.SellOut {
		b.Client.Logger.Infof("start positions sell out...")
		err := b.executor.SellOut()
		if err != nil {
			return err
		}
	}

	// так как исполнитель тоже слушает стримы, его нужно явно остановить
	b.executor.Stop()

	wg.Wait()
	return nil
}

// Stop - Принудительное завершение работы бота, если SellOut = true, то бот выходит из всех активных позиций, которые он открыл
func (b *Bot) Stop() {
	b.cancelBot()
}

// HandleOrderBooks - нужно вызвать асинхронно, будет писать в канал id инструментов, которые нужно купить или продать
func (b *Bot) HandleOrderBooks(ctx context.Context, orderBooks chan OrderBook) (float64, error) {
	var totalProfit float64
	for {
		select {
		case <-ctx.Done():
			return totalProfit, nil
		case ob, ok := <-orderBooks:
			if !ok {
				return totalProfit, nil
			}
			ratio := b.checkRatio(ob)
			if ratio > b.StrategyConfig.BuyRatio {
				err := b.executor.Buy(ob.InstrumentUid)
				if err != nil {
					return totalProfit, err
				}
			} else if 1/ratio > b.StrategyConfig.SellRatio {
				profit, err := b.executor.Sell(ob.InstrumentUid)
				if err != nil {
					return totalProfit, err
				}
				if profit > 0 {
					b.Client.Logger.Infof("profit = %.9f", profit)
					totalProfit += profit
				}
			}
		}
	}
}

// checkRate - возвращает значения коэффициента count(ask) / count(bid)
func (b *Bot) checkRatio(ob OrderBook) float64 {
	sell := ordersCount(ob.Asks)
	buy := ordersCount(ob.Bids)
	return float64(buy) / float64(sell)
}

// ordersCount - возвращает кол-во заявок из слайса ордеров
func ordersCount(o []Order) int64 {
	var count int64
	for _, order := range o {
		count += order.Quantity
	}
	return count
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

// transformOrderBook - Преобразование стакана в нужный формат
func transformOrderBook(input *pb.OrderBook) OrderBook {
	depth := input.GetDepth()
	bids := make([]Order, 0, depth)
	asks := make([]Order, 0, depth)
	for _, o := range input.GetBids() {
		bids = append(bids, Order{
			Price:    o.GetPrice().ToFloat(),
			Quantity: o.GetQuantity(),
		})
	}
	for _, o := range input.GetAsks() {
		asks = append(asks, Order{
			Price:    o.GetPrice().ToFloat(),
			Quantity: o.GetQuantity(),
		})
	}
	return OrderBook{
		Figi:          input.GetFigi(),
		InstrumentUid: input.GetInstrumentUid(),
		Depth:         depth,
		IsConsistent:  input.GetIsConsistent(),
		TimeUnix:      input.GetTime().AsTime().Unix(),
		LimitUp:       input.GetLimitUp().ToFloat(),
		LimitDown:     input.GetLimitDown().ToFloat(),
		Bids:          bids,
		Asks:          asks,
	}
}
