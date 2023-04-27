package investgo

import (
	"context"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc"
)

type OperationsStreamClient struct {
	conn     *grpc.ClientConn
	config   Config
	logger   Logger
	ctx      context.Context
	pbClient pb.OperationsStreamServiceClient
}

// PortfolioStream - Server-side stream обновлений портфеля
func (o *OperationsStreamClient) PortfolioStream(accounts []string) (*PortfolioStream, error) {
	ctx, cancel := context.WithCancel(o.ctx)
	stream, err := o.pbClient.PortfolioStream(ctx, &pb.PortfolioStreamRequest{
		Accounts: accounts,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &PortfolioStream{
		stream:           stream,
		operationsClient: o,
		portfolios:       make(chan *pb.PortfolioResponse),
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}

// PositionsStream - Server-side stream обновлений информации по изменению позиций портфеля
func (o *OperationsStreamClient) PositionsStream(accounts []string) (*PositionsStream, error) {
	ctx, cancel := context.WithCancel(o.ctx)
	stream, err := o.pbClient.PositionsStream(ctx, &pb.PositionsStreamRequest{
		Accounts: accounts,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &PositionsStream{
		stream:           stream,
		operationsClient: o,
		positions:        make(chan *pb.PositionData),
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}
