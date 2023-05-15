package investgo

import (
	"context"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"github.com/tinkoff/invest-api-go-sdk/retry"
	"google.golang.org/grpc"
)

type MDStreamClient struct {
	conn     *grpc.ClientConn
	config   Config
	logger   Logger
	ctx      context.Context
	pbClient pb.MarketDataStreamServiceClient
}

// MarketDataStream - метод возвращает стрим биржевой информации
func (c *MDStreamClient) MarketDataStream() (*MDStream, error) {
	ctx, cancel := context.WithCancel(c.ctx)
	mds := &MDStream{
		stream:        nil,
		mdsClient:     c,
		ctx:           ctx,
		cancel:        cancel,
		candle:        make(chan *pb.Candle, 1),
		trade:         make(chan *pb.Trade, 1),
		orderBook:     make(chan *pb.OrderBook, 1),
		lastPrice:     make(chan *pb.LastPrice, 1),
		tradingStatus: make(chan *pb.TradingStatus, 1),
		subs: subscriptions{
			candles:         make(map[string]pb.SubscriptionInterval, 0),
			orderBooks:      make(map[string]int32, 0),
			trades:          make(map[string]struct{}, 0),
			tradingStatuses: make(map[string]struct{}, 0),
			lastPrices:      make(map[string]struct{}, 0),
		},
	}

	stream, err := c.pbClient.MarketDataStream(ctx, retry.WithOnRetryCallback(mds.restart))
	if err != nil {
		cancel()
		return nil, err
	}
	mds.stream = stream
	return mds, nil
}
