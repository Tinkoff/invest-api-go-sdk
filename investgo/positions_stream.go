package investgo

import (
	"context"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PositionsStream struct {
	stream           pb.OperationsStreamService_PositionsStreamClient
	operationsClient *OperationsStreamClient

	ctx    context.Context
	cancel context.CancelFunc

	positions chan *pb.PositionData
}

// Positions -  Метод возвращает канал для чтения обновлений информации по изменению позиций портфеля
func (p *PositionsStream) Positions() <-chan *pb.PositionData {
	return p.positions
}

// Listen - метод начинает слушать стрим и отправлять информацию в канал, для получения канала: Positions()
func (p *PositionsStream) Listen() error {
	defer p.shutdown()
	for {
		select {
		case <-p.ctx.Done():
			return nil
		default:
			resp, err := p.stream.Recv()
			if err != nil {
				switch {
				case status.Code(err) == codes.Canceled:
					p.operationsClient.logger.Infof("Stop listening positions")
					return nil
				default:
					return err
				}
			} else {
				switch resp.GetPayload().(type) {
				case *pb.PositionsStreamResponse_Position:
					p.positions <- resp.GetPosition()
				default:
					p.operationsClient.logger.Infof("Info from Positions stream %v", resp.String())
				}
			}
		}
	}
}

func (p *PositionsStream) shutdown() {
	p.operationsClient.logger.Infof("Close positions stream")
	close(p.positions)
}

// Stop - Завершение работы стрима
func (p *PositionsStream) Stop() {
	p.cancel()
}
