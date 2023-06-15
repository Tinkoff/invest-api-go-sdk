package bot

import (
	"context"
)

type OrderBookStrategyConfig struct {
	Instruments []string
	Depth       int32
	//  Если кол-во бид/аск больше чем BuyRatio - покупаем
	BuyRatio float64
	//  Если кол-во бид/аск меньше чем SellRatio - продаем
	SellRatio float64
}

type OrderBookStrategy struct {
	config   OrderBookStrategyConfig
	executor *Executor
}

func NewOrderBookStrategy(config OrderBookStrategyConfig, e *Executor) *OrderBookStrategy {
	return &OrderBookStrategy{
		config:   config,
		executor: e,
	}
}

// HandleOrderBooks - нужно вызвать асинхронно, будет писать в канал id инструментов, которые нужно купить или продать
func (o *OrderBookStrategy) HandleOrderBooks(ctx context.Context, orderBooks chan OrderBook) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ob, ok := <-orderBooks:
			if !ok {
				return nil
			}
			ratio := o.checkRatio(ob)
			if ratio > o.config.BuyRatio {
				err := o.executor.Buy(ob.InstrumentUid)
				if err != nil {
					return err
				}
			} else if 1/ratio > o.config.SellRatio {
				err := o.executor.Sell(ob.InstrumentUid)
				if err != nil {
					return err
				}
			}
		}
	}
}

// checkRate - возвращает значения коэффициента count(ask) / count(bid)
func (o *OrderBookStrategy) checkRatio(ob OrderBook) float64 {
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
