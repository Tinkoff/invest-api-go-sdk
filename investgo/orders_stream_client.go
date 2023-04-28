package investgo

import (
	"context"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc"
)

type OrdersStreamClient struct {
	conn     *grpc.ClientConn
	config   Config
	logger   Logger
	ctx      context.Context
	pbClient pb.OrdersStreamServiceClient
}

// TradesStream - Стрим сделок по запрашиваемым аккаунтам
func (o *OrdersStreamClient) TradesStream(accounts []string) (*TradesStream, error) {
	ctx, cancel := context.WithCancel(o.ctx)
	stream, err := o.pbClient.TradesStream(ctx, &pb.TradesStreamRequest{
		Accounts: accounts,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &TradesStream{
		stream:       stream,
		ordersClient: o,
		trades:       make(chan *pb.OrderTrades),
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}
