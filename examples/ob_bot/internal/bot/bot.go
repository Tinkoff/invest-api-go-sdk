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

type Bot struct {
	StrategyConfig OrderBookStrategyConfig
	Client         *investgo.Client

	ctx          context.Context
	cancelClient context.CancelFunc
	cancelBot    context.CancelFunc

	executor *Executor
}

// NewBot - Создание экземпляра бота на стакане
// dd - дедлайн работы бота для интрадей торговли
// каждый бот создает своего клиента для работы с investAPI
func NewBot(ctx context.Context, dd time.Time, sdkConf investgo.Config, l investgo.Logger, config OrderBookStrategyConfig) (*Bot, error) {
	// Контекст для клиента с дедлайном. cancelClient - принудительно завершает работу клиента и бота, после отмены контекста
	// клиента невозможно выполнить никакие запросы к апи.
	clientCtx, cancelClient := context.WithDeadline(ctx, dd)
	// Контекст для бота - потомок контекста клиента. cancelBot - завершает работу стратегии и чтение из стрима, т.е
	// всей функции Run, но при этом остается активным клиент для апи.
	botCtx, cancelBot := context.WithCancel(clientCtx)
	// если нужно выходить из позиций, то бот будет завершать свою работу раньше чем дедлайн
	if config.SellOut {
		botCtx, cancelBot = context.WithDeadline(botCtx, dd.Add(-config.SellOutAhead))
	}

	c, err := investgo.NewClient(clientCtx, sdkConf, l)
	if err != nil {
		cancelClient()
		cancelBot()
		return nil, err
	}
	return &Bot{
		Client:         c,
		StrategyConfig: config,
		ctx:            botCtx,
		cancelBot:      cancelBot,
		cancelClient:   cancelClient,
	}, nil
}

// Run - Запуск бота
func (b *Bot) Run() error {
	defer func() {
		err := b.shutdown()
		if err != nil {
			b.Client.Logger.Errorf("bot shutdown: %v", err.Error())
		}
	}()

	wg := &sync.WaitGroup{}

	instrumentService := b.Client.NewInstrumentsServiceClient()
	instruments := make(map[string]Instrument, len(b.StrategyConfig.Instruments))
	for _, instrument := range b.StrategyConfig.Instruments {
		// в данном случае ключ это uid, поэтому используем LotByUid()
		resp, err := instrumentService.InstrumentByUid(instrument)
		if err != nil {
			return err
		}
		instruments[instrument] = Instrument{
			quantity: QUANTITY,
			inStock:  false,
			buyPrice: 0,
			lot:      resp.GetInstrument().GetLot(),
			currency: resp.GetInstrument().GetCurrency(),
		}
	}
	lastPrices := make(map[string]float64, len(b.StrategyConfig.Instruments))

	executor := NewExecutor(b.Client, instruments, lastPrices, b.StrategyConfig.MinProfit)
	b.executor = executor

	// создаем стратегию
	s := NewOrderBookStrategy(b.StrategyConfig, executor)

	// инфраструктура для работы стратегии: запрос, получение, преобразование рыночных данных
	MarketDataStreamService := b.Client.NewMarketDataStreamClient()
	stream, err := MarketDataStreamService.MarketDataStream()
	if err != nil {
		return err
	}
	pbOrderBooks, err := stream.SubscribeOrderBook(s.config.Instruments, s.config.Depth)
	if err != nil {
		return err
	}

	lastPricesChan, err := stream.SubscribeLastPrice(s.config.Instruments)
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
	// defer close(orderBooks)

	// чтение из стрима
	wg.Add(1)
	go func(ctx context.Context) {
		defer func() {
			close(orderBooks)
			stream.Stop()
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
		err := s.HandleOrderBooks(ctx, orderBooks)
		if err != nil {
			b.Client.Logger.Errorf(err.Error())
		}
	}(b.ctx)

	// Завершение работы бота по его контексту: вызов Stop() или отмена по дедлайну
	for {
		select {
		case <-b.ctx.Done():
			b.Client.Logger.Infof("stop bot on order book...")

			if b.StrategyConfig.SellOut {
				b.Client.Logger.Infof("start positions sell out...")
				err := b.executor.SellOut()
				if err != nil {
					return err
				}
			}

			stream.Stop()

			break
		}
		break
	}

	wg.Wait()
	return nil
}

func (b *Bot) shutdown() error {
	// TODO positions sell and client shutdown
	// если Run завершился с ошибкой, то эта функция отменит контекст клиента, внутри бота и закроет соединение
	b.cancelClient()
	return b.Client.Stop()
}

// Stop - Принудительное завершение работы бота, если = true, то бот выходит из всех активных позиций по счету
func (b *Bot) Stop() {
	b.cancelBot()
}

func (b *Bot) BackTest() error {
	// качаем из бд стаканы сбера и испытываем стратегию
	return nil
}

// Преобразование стакана в нужный формат
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
