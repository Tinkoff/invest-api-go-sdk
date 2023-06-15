package bot

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"sync"
	"time"
)

const QUANTITY = 1

type Bot struct {
	StrategyConfig OrderBookStrategyConfig
	Client         *investgo.Client

	ctx          context.Context
	cancelClient context.CancelFunc
	cancelBot    context.CancelFunc

	executor *Executor
}

func NewBot(ctx context.Context, dd time.Time, sdkConf investgo.Config, l investgo.Logger, config OrderBookStrategyConfig) (*Bot, error) {
	// контекст для клиента с дедлайном
	clientCtx, cancelClient := context.WithDeadline(ctx, dd)
	// контекст для бота - потомок контекста клиента
	botCtx, cancelBot := context.WithCancel(clientCtx)
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

func (b *Bot) Run() error {
	defer func() {
		err := b.shutdown()
		if err != nil {
			b.Client.Logger.Errorf("bot shutdown: %v", err.Error())
		}
	}()

	wg := &sync.WaitGroup{}

	instruments := make(map[string]Instrument, len(b.StrategyConfig.Instruments))
	for _, instrument := range b.StrategyConfig.Instruments {
		instruments[instrument] = Instrument{quantity: QUANTITY}
	}
	lastPrices := make(map[string]float64, len(b.StrategyConfig.Instruments))

	executor := NewExecutor(b.Client, instruments, lastPrices)
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

	wg.Wait()

	return nil
}

func (b *Bot) shutdown() error {
	// TODO positions sell and client shutdown
	return b.Client.Stop()
}

// Stop - принудительное завершение работы бота
func (b *Bot) Stop() {
	// в конце убиваем клиента
	defer b.cancelClient()
	// сначала завершаем работу стратегии (всех рутин от бота)
	b.Client.Logger.Infof("Stop bot on order book...")
	b.cancelBot()
	err := b.executor.SellOut()
	if err != nil {
		b.Client.Logger.Errorf(err.Error())
	}
	// TODO graceful stop
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
