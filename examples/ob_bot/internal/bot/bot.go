package bot

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"sync"
	"time"
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
	// (Дедлайн интрадей торговли - SellOutAhead) - это момент времени, когда бот начнет продавать
	// все активные позиции
	SellOutAhead time.Duration
}

type Bot struct {
	StrategyConfig OrderBookStrategyConfig
	Client         *investgo.Client

	ctx       context.Context
	cancelBot context.CancelFunc

	executor *Executor
}

// NewBot - Создание экземпляра бота на стакане
// dd - дедлайн работы бота для интрадей торговли
// каждый бот создает своего клиента для работы с investAPI
func NewBot(ctx context.Context, c *investgo.Client, dd time.Time, config OrderBookStrategyConfig) (*Bot, error) {
	botCtx, cancelBot := context.WithDeadline(ctx, dd)
	// если нужно выходить из позиций, то бот будет завершать свою работу раньше чем дедлайн
	if config.SellOut {
		botCtx, cancelBot = context.WithDeadline(botCtx, dd.Add(-config.SellOutAhead))
	}

	return &Bot{
		Client:         c,
		StrategyConfig: config,
		ctx:            botCtx,
		cancelBot:      cancelBot,
	}, nil
}

// Run - Запуск бота
func (b *Bot) Run() error {
	wg := &sync.WaitGroup{}
	// по конфигу стратегии заполняем map для executor
	instrumentService := b.Client.NewInstrumentsServiceClient()
	instruments := make(map[string]Instrument, len(b.StrategyConfig.Instruments))

	for _, instrument := range b.StrategyConfig.Instruments {
		// в данном случае ключ это uid, поэтому используем LotByUid()
		resp, err := instrumentService.InstrumentByUid(instrument)
		if err != nil {
			return err
		}
		instruments[instrument] = Instrument{
			quantity:   QUANTITY,
			inStock:    false,
			entryPrice: 0,
			lot:        resp.GetInstrument().GetLot(),
			currency:   resp.GetInstrument().GetCurrency(),
		}
	}
	lastPrices := make(map[string]float64, len(b.StrategyConfig.Instruments))

	executor := NewExecutor(b.Client, instruments, lastPrices, b.StrategyConfig.MinProfit)
	b.executor = executor

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

	lastPricesChan, err := stream.SubscribeLastPrice(b.StrategyConfig.Instruments)
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
		defer func() {
			wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-pbOrderBooks:
				if !ok {
					return
				}
				orderBooks <- transformOrderBook(ob)
			case lp, ok := <-lastPricesChan:
				if !ok {
					return
				}
				// обновление данных в мапе последних цен
				lastPrices[lp.GetInstrumentUid()] = lp.GetPrice().ToFloat()
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
		b.Client.Logger.Infof("profit by strategy = %v", profit)
	}(b.ctx)

	// Завершение работы бота по его контексту: вызов Stop() или отмена по дедлайну
	<-b.ctx.Done()
	b.Client.Logger.Infof("stop bot on order book...")

	// стримы работают на контексте клиента, завершать их нужно явно
	stream.Stop()

	if b.StrategyConfig.SellOut {
		b.Client.Logger.Infof("start positions sell out...")
		err := b.executor.SellOut()
		if err != nil {
			return err
		}
	}

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

func ordersCount(o []Order) int64 {
	var count int64
	for _, order := range o {
		count += order.Quantity
	}
	return count
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
