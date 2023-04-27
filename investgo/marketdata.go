package investgo

import (
	"context"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"time"
)

type MarketDataServiceClient struct {
	conn     *grpc.ClientConn
	config   Config
	logger   Logger
	ctx      context.Context
	pbClient pb.MarketDataServiceClient
}

// GetCandles - Метод запроса исторических свечей по инструменту
func (md *MarketDataServiceClient) GetCandles(instrumentId string, interval pb.CandleInterval, from, to time.Time) (*GetCandlesResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetCandles(md.ctx, &pb.GetCandlesRequest{
		From:         TimeToTimestamp(from),
		To:           TimeToTimestamp(to),
		Interval:     interval,
		InstrumentId: instrumentId,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetCandlesResponse{
		GetCandlesResponse: resp,
		Header:             header,
	}, err
}

// GetLastPrices - Метод запроса цен последних сделок по инструментам
func (md *MarketDataServiceClient) GetLastPrices(instrumentIds []string) (*GetLastPricesResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetLastPrices(md.ctx, &pb.GetLastPricesRequest{
		InstrumentId: instrumentIds,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetLastPricesResponse{
		GetLastPricesResponse: resp,
		Header:                header,
	}, err
}

// GetOrderBook - Метод получения стакана по инструменту
func (md *MarketDataServiceClient) GetOrderBook(instrumentId string, depth int32) (*GetOrderBookResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetOrderBook(md.ctx, &pb.GetOrderBookRequest{
		Depth:        depth,
		InstrumentId: instrumentId,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetOrderBookResponse{
		GetOrderBookResponse: resp,
		Header:               header,
	}, err
}

// GetTradingStatus - Метод запроса статуса торгов по инструменту
func (md *MarketDataServiceClient) GetTradingStatus(instrumentId string) (*GetTradingStatusResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetTradingStatus(md.ctx, &pb.GetTradingStatusRequest{
		InstrumentId: instrumentId,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetTradingStatusResponse{
		GetTradingStatusResponse: resp,
		Header:                   header,
	}, err
}

// GetTradingStatuses - Метод запроса статуса торгов по инструментам
func (md *MarketDataServiceClient) GetTradingStatuses(instrumentIds []string) (*GetTradingStatusesResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetTradingStatuses(md.ctx, &pb.GetTradingStatusesRequest{
		InstrumentId: instrumentIds,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetTradingStatusesResponse{
		GetTradingStatusesResponse: resp,
		Header:                     header,
	}, err
}

// GetLastTrades - Метод запроса обезличенных сделок за последний час
func (md *MarketDataServiceClient) GetLastTrades(instrumentId string, from, to time.Time) (*GetLastTradesResponse, error) {
	var header, trailer metadata.MD
	resp, err := md.pbClient.GetLastTrades(md.ctx, &pb.GetLastTradesRequest{
		From:         TimeToTimestamp(from),
		To:           TimeToTimestamp(to),
		InstrumentId: instrumentId,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetLastTradesResponse{
		GetLastTradesResponse: resp,
		Header:                header,
	}, err
}

// GetClosePrices - Метод запроса цен закрытия торговой сессии по инструментам
func (md *MarketDataServiceClient) GetClosePrices(instrumentIds []string) (*GetClosePricesResponse, error) {
	var header, trailer metadata.MD
	instruments := make([]*pb.InstrumentClosePriceRequest, 0, len(instrumentIds))
	for _, id := range instrumentIds {
		instruments = append(instruments, &pb.InstrumentClosePriceRequest{InstrumentId: id})
	}
	resp, err := md.pbClient.GetClosePrices(md.ctx, &pb.GetClosePricesRequest{
		Instruments: instruments,
	}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		header = trailer
	}
	return &GetClosePricesResponse{
		GetClosePricesResponse: resp,
		Header:                 header,
	}, err
}
