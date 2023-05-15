package investgo

import (
	"context"
	"fmt"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"github.com/tinkoff/invest-api-go-sdk/retry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MDStream struct {
	stream    pb.MarketDataStreamService_MarketDataStreamClient
	mdsClient *MDStreamClient

	ctx    context.Context
	cancel context.CancelFunc

	candle        chan *pb.Candle
	trade         chan *pb.Trade
	orderBook     chan *pb.OrderBook
	lastPrice     chan *pb.LastPrice
	tradingStatus chan *pb.TradingStatus

	subs subscriptions
}

type subscriptions struct {
	candles         map[string]pb.SubscriptionInterval
	orderBooks      map[string]int32
	trades          map[string]struct{}
	tradingStatuses map[string]struct{}
	lastPrices      map[string]struct{}
}

// SubscribeCandle - Метод подписки на свечи с заданным интервалом
func (mds *MDStream) SubscribeCandle(ids []string, interval pb.SubscriptionInterval) (<-chan *pb.Candle, error) {
	err := mds.sendCandlesReq(ids, interval, pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		mds.subs.candles[id] = interval
	}
	return mds.candle, nil
}

// UnSubscribeCandle - Метод отписки от свечей
func (mds *MDStream) UnSubscribeCandle(ids []string, interval pb.SubscriptionInterval) error {
	err := mds.sendCandlesReq(ids, interval, pb.SubscriptionAction_SUBSCRIPTION_ACTION_UNSUBSCRIBE)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(mds.subs.candles, id)
	}
	return nil
}

func (mds *MDStream) sendCandlesReq(ids []string, interval pb.SubscriptionInterval, act pb.SubscriptionAction) error {
	instruments := make([]*pb.CandleInstrument, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, &pb.CandleInstrument{
			InstrumentId: id,
			Interval:     interval,
		})
	}

	WCFlag := interval == pb.SubscriptionInterval_SUBSCRIPTION_INTERVAL_ONE_MINUTE

	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_SubscribeCandlesRequest{
			SubscribeCandlesRequest: &pb.SubscribeCandlesRequest{
				SubscriptionAction: act,
				Instruments:        instruments,
				WaitingClose:       WCFlag,
			}}})
}

// SubscribeOrderBook - метод подписки на стаканы инструментов с одинаковой глубиной
func (mds *MDStream) SubscribeOrderBook(ids []string, depth int32) (<-chan *pb.OrderBook, error) {
	err := mds.sendOrderBookReq(ids, depth, pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		mds.subs.orderBooks[id] = depth
	}
	return mds.orderBook, nil
}

// UnSubscribeOrderBook - метод отдписки от стаканов инструментов
func (mds *MDStream) UnSubscribeOrderBook(ids []string) error {
	err := mds.sendOrderBookReq(ids, 0, pb.SubscriptionAction_SUBSCRIPTION_ACTION_UNSUBSCRIBE)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(mds.subs.orderBooks, id)
	}
	return nil
}

func (mds *MDStream) sendOrderBookReq(ids []string, depth int32, act pb.SubscriptionAction) error {
	instruments := make([]*pb.OrderBookInstrument, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, &pb.OrderBookInstrument{
			Depth:        depth,
			InstrumentId: id,
		})
	}
	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_SubscribeOrderBookRequest{
			SubscribeOrderBookRequest: &pb.SubscribeOrderBookRequest{
				SubscriptionAction: act,
				Instruments:        instruments,
			}}})
}

// SubscribeTrade - метод подписки на ленту обезличенных сделок
func (mds *MDStream) SubscribeTrade(ids []string) (<-chan *pb.Trade, error) {
	err := mds.sendTradesReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		mds.subs.trades[id] = struct{}{}
	}
	return mds.trade, nil
}

// UnSubscribeTrade - метод отписки от ленты обезличенных сделок
func (mds *MDStream) UnSubscribeTrade(ids []string) error {
	err := mds.sendTradesReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_UNSUBSCRIBE)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(mds.subs.trades, id)
	}
	return nil
}

func (mds *MDStream) sendTradesReq(ids []string, act pb.SubscriptionAction) error {
	instruments := make([]*pb.TradeInstrument, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, &pb.TradeInstrument{
			InstrumentId: id,
		})
	}
	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_SubscribeTradesRequest{
			SubscribeTradesRequest: &pb.SubscribeTradesRequest{
				SubscriptionAction: act,
				Instruments:        instruments,
			}}})
}

// SubscribeInfo - метод подписки на торговые статусы инструментов
func (mds *MDStream) SubscribeInfo(ids []string) (<-chan *pb.TradingStatus, error) {
	err := mds.sendInfoReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		mds.subs.tradingStatuses[id] = struct{}{}
	}
	return mds.tradingStatus, nil
}

// UnSubscribeInfo - метод отписки от торговых статусов инструментов
func (mds *MDStream) UnSubscribeInfo(ids []string) error {
	err := mds.sendInfoReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_UNSUBSCRIBE)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(mds.subs.tradingStatuses, id)
	}
	return nil
}

func (mds *MDStream) sendInfoReq(ids []string, act pb.SubscriptionAction) error {
	instruments := make([]*pb.InfoInstrument, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, &pb.InfoInstrument{
			InstrumentId: id,
		})
	}
	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_SubscribeInfoRequest{
			SubscribeInfoRequest: &pb.SubscribeInfoRequest{
				SubscriptionAction: act,
				Instruments:        instruments,
			}}})
}

// SubscribeLastPrice - метод подписки на последние цены инструментов
func (mds *MDStream) SubscribeLastPrice(ids []string) (<-chan *pb.LastPrice, error) {
	err := mds.sendLastPriceReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_SUBSCRIBE)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		mds.subs.lastPrices[id] = struct{}{}
	}
	return mds.lastPrice, nil
}

// UnSubscribeLastPrice - метод отписки от последних цен инструментов
func (mds *MDStream) UnSubscribeLastPrice(ids []string) error {
	err := mds.sendLastPriceReq(ids, pb.SubscriptionAction_SUBSCRIPTION_ACTION_UNSUBSCRIBE)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(mds.subs.lastPrices, id)
	}
	return nil
}

func (mds *MDStream) sendLastPriceReq(ids []string, act pb.SubscriptionAction) error {
	instruments := make([]*pb.LastPriceInstrument, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, &pb.LastPriceInstrument{
			InstrumentId: id,
		})
	}
	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_SubscribeLastPriceRequest{
			SubscribeLastPriceRequest: &pb.SubscribeLastPriceRequest{
				SubscriptionAction: act,
				Instruments:        instruments,
			}}})
}

// GetMySubscriptions - метод получения подписок в рамках данного стрима
func (mds *MDStream) GetMySubscriptions() error {
	return mds.stream.Send(&pb.MarketDataRequest{
		Payload: &pb.MarketDataRequest_GetMySubscriptions{
			GetMySubscriptions: &pb.GetMySubscriptions{}}})
}

func (mds *MDStream) Listen() error {
	defer mds.shutdown()
	for {
		select {
		case <-mds.ctx.Done():
			mds.mdsClient.logger.Infof("stop listening")
			return nil
		default:
			resp, err := mds.stream.Recv()
			if err != nil {
				// если ошибка связана с завершением контекста, обрабатываем ее
				switch {
				case status.Code(err) == codes.Canceled:
					mds.mdsClient.logger.Infof("stop listening")
					return nil
				default:
					return err
				}
			} else {
				// логика определения того что пришло и отправка информации в нужный канал
				mds.sendRespToChannel(resp)
			}
		}
	}
}

func (mds *MDStream) sendRespToChannel(resp *pb.MarketDataResponse) {
	switch resp.GetPayload().(type) {
	case *pb.MarketDataResponse_Candle:
		mds.candle <- resp.GetCandle()
	case *pb.MarketDataResponse_Orderbook:
		mds.orderBook <- resp.GetOrderbook()
	case *pb.MarketDataResponse_Trade:
		mds.trade <- resp.GetTrade()
	case *pb.MarketDataResponse_LastPrice:
		mds.lastPrice <- resp.GetLastPrice()
	case *pb.MarketDataResponse_TradingStatus:
		mds.tradingStatus <- resp.GetTradingStatus()
	default:
		mds.mdsClient.logger.Infof("Info from MD stream %v", resp.String())
	}
}

func (mds *MDStream) shutdown() {
	mds.mdsClient.logger.Infof("Close market data stream")
	close(mds.candle)
	close(mds.trade)
	close(mds.lastPrice)
	close(mds.orderBook)
	close(mds.tradingStatus)
}

// Stop - Завершение работы стрима
func (mds *MDStream) Stop() {
	mds.cancel()
}

// UnSubscribeAll - Метод отписки от всей информации, отслеживаемой на данный момент
func (mds *MDStream) UnSubscribeAll() error {
	ids := make([]string, 0)
	if len(mds.subs.candles) > 0 {
		intervals := make(map[pb.SubscriptionInterval][]string, 0)

		for id, interval := range mds.subs.candles {
			intervals[interval] = append(intervals[interval], id)
			delete(mds.subs.candles, id)
		}
		for interval, ids := range intervals {
			err := mds.UnSubscribeCandle(ids, interval)
			if err != nil {
				return err
			}
		}
	}

	if len(mds.subs.trades) > 0 {
		for id := range mds.subs.trades {
			ids = append(ids, id)
			delete(mds.subs.trades, id)
		}
		err := mds.UnSubscribeTrade(ids)
		if err != nil {
			return err
		}
		ids = nil
	}

	if len(mds.subs.tradingStatuses) > 0 {
		for id := range mds.subs.tradingStatuses {
			ids = append(ids, id)
			delete(mds.subs.tradingStatuses, id)
		}
		err := mds.UnSubscribeInfo(ids)
		if err != nil {
			return err
		}
		ids = nil
	}

	if len(mds.subs.lastPrices) > 0 {
		for id := range mds.subs.lastPrices {
			ids = append(ids, id)
			delete(mds.subs.lastPrices, id)
		}
		err := mds.UnSubscribeLastPrice(ids)
		if err != nil {
			return err
		}
		ids = nil
	}

	if len(mds.subs.orderBooks) > 0 {
		for id := range mds.subs.orderBooks {
			ids = append(ids, id)
			delete(mds.subs.orderBooks, id)
		}
		err := mds.UnSubscribeOrderBook(ids)
		if err != nil {
			return err
		}
	}

	return nil
}

func (mds *MDStream) restart(ctx context.Context, attempt uint, err error) {
	fmt.Printf("Try to retry err = %v, attemt = %v\n", err.Error(), attempt)
	mds.stream, err = mds.mdsClient.pbClient.MarketDataStream(mds.ctx, retry.WithOnRetryCallback(mds.restart))
	if err != nil {
		mds.mdsClient.logger.Errorf("md stream restart fail %v", err.Error())
	}
	err = mds.reSubscribeAll()
	if err != nil {
		mds.mdsClient.logger.Errorf("md stream restart fail %v", err.Error())
	}
}

func (mds *MDStream) reSubscribeAll() error {
	ids := make([]string, 0)
	if len(mds.subs.candles) > 0 {
		intervals := make(map[pb.SubscriptionInterval][]string, 0)

		for id, interval := range mds.subs.candles {
			intervals[interval] = append(intervals[interval], id)
		}
		for interval, ids := range intervals {
			_, err := mds.SubscribeCandle(ids, interval)
			if err != nil {
				return err
			}
		}
	}

	if len(mds.subs.trades) > 0 {
		for id := range mds.subs.trades {
			ids = append(ids, id)
		}
		_, err := mds.SubscribeTrade(ids)
		if err != nil {
			return err
		}
		ids = nil
	}

	if len(mds.subs.tradingStatuses) > 0 {
		for id := range mds.subs.tradingStatuses {
			ids = append(ids, id)
		}
		_, err := mds.SubscribeInfo(ids)
		if err != nil {
			return err
		}
		ids = nil
	}

	if len(mds.subs.lastPrices) > 0 {
		for id := range mds.subs.lastPrices {
			ids = append(ids, id)
		}
		_, err := mds.SubscribeLastPrice(ids)
		if err != nil {
			return err
		}
	}

	if len(mds.subs.orderBooks) > 0 {
		orderBooks := make(map[int32][]string, 0)

		for id, depth := range mds.subs.orderBooks {
			orderBooks[depth] = append(orderBooks[depth], id)
		}

		for depth, ids := range orderBooks {
			_, err := mds.SubscribeOrderBook(ids, depth)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
