package bot

import (
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
)

const MIN_PROFIT = 1

type Instrument struct {
	quantity int64
	lot      int32
	currency string

	inStock  bool
	buyPrice float64
}

type Executor struct {
	instruments map[string]Instrument

	// lastPrices - read only for executor
	lastPrices map[string]float64

	client            *investgo.Client
	ordersService     *investgo.OrdersServiceClient
	operationsService *investgo.OperationsServiceClient
}

func NewExecutor(c *investgo.Client, ids map[string]Instrument, lp map[string]float64) *Executor {
	return &Executor{
		instruments:       ids,
		lastPrices:        lp,
		client:            c,
		ordersService:     c.NewOrdersServiceClient(),
		operationsService: c.NewOperationsServiceClient(),
	}
}

func (e *Executor) Buy(id string) error {
	currentInstrument := e.instruments[id]
	if currentInstrument.inStock {
		return nil
	}
	if !e.possibleToBuy(id) {
		return nil
	}
	resp, err := e.ordersService.Buy(&investgo.PostOrderRequestShort{
		InstrumentId: id,
		Quantity:     currentInstrument.quantity,
		Price:        nil,
		AccountId:    e.client.Config.AccountId,
		OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
		OrderId:      investgo.CreateUid(),
	})
	if err != nil {
		return err
	}
	if resp.GetExecutionReportStatus() == pb.OrderExecutionReportStatus_EXECUTION_REPORT_STATUS_FILL {
		currentInstrument.inStock = true
		currentInstrument.buyPrice = resp.GetExecutedOrderPrice().ToFloat()
	}
	e.instruments[id] = currentInstrument
	e.client.Logger.Infof("Buy with %v, price %v", resp.GetFigi(), resp.GetExecutedOrderPrice().ToFloat())
	return nil
}

func (e *Executor) Sell(id string) error {
	currentInstrument := e.instruments[id]
	if !currentInstrument.inStock {
		return nil
	}
	if profitable := e.isProfitable(id); !profitable {
		return nil
	}

	resp, err := e.ordersService.Sell(&investgo.PostOrderRequestShort{
		InstrumentId: id,
		Quantity:     currentInstrument.quantity,
		Price:        nil,
		AccountId:    e.client.Config.AccountId,
		OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
		OrderId:      investgo.CreateUid(),
	})
	if err != nil {
		return err
	}
	if resp.GetExecutionReportStatus() == pb.OrderExecutionReportStatus_EXECUTION_REPORT_STATUS_FILL {
		currentInstrument.inStock = false
		e.client.Logger.Infof("profit = %.9f", resp.GetExecutedOrderPrice().ToFloat()-currentInstrument.buyPrice)
	}
	e.client.Logger.Infof("Sell with %v, price %v", resp.GetFigi(), resp.GetExecutedOrderPrice().ToFloat())
	e.instruments[id] = currentInstrument
	return nil
}

func (e *Executor) isProfitable(id string) bool {
	return (e.lastPrices[id] - e.instruments[id].buyPrice) > MIN_PROFIT
}

func (e *Executor) possibleToBuy(id string) bool {
	// требуемая сумма для покупки
	// кол-во лотов * лотность * стоимость 1 инструмента
	required := float64(e.instruments[id].quantity) * float64(e.instruments[id].lot) * e.lastPrices[id]
	resp, err := e.operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		e.client.Logger.Errorf(err.Error())
	}
	money := resp.GetMoney()
	var moneyInFloat float64
	for _, m := range money {
		if m.GetCurrency() == e.instruments[id].currency {
			moneyInFloat = m.ToFloat()
		}
	}
	// TODO сравнение дробных чисел
	return moneyInFloat > required
}

func (e *Executor) possibleToSell() {

}

// SellOut - продать все текущие позиции
func (e *Executor) SellOut() error {
	resp, err := e.operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		return err
	}
	// TODO for futures and options
	securities := resp.GetSecurities()
	for _, security := range securities {
		balanceInLots := security.GetBalance() / int64(e.instruments[security.GetInstrumentUid()].lot)
		if balanceInLots < 0 {
			resp, err := e.ordersService.Buy(&investgo.PostOrderRequestShort{
				InstrumentId: security.GetInstrumentUid(),
				Quantity:     -balanceInLots,
				Price:        nil,
				AccountId:    e.client.Config.AccountId,
				OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
				OrderId:      investgo.CreateUid(),
			})
			if err != nil {
				e.client.Logger.Errorf(investgo.MessageFromHeader(resp.GetHeader()))
				return err
			}
		} else {
			resp, err := e.ordersService.Sell(&investgo.PostOrderRequestShort{
				InstrumentId: security.GetInstrumentUid(),
				Quantity:     balanceInLots,
				Price:        nil,
				AccountId:    e.client.Config.AccountId,
				OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
				OrderId:      investgo.CreateUid(),
			})
			if err != nil {
				e.client.Logger.Errorf(investgo.MessageFromHeader(resp.GetHeader()))
				return err
			}
		}

	}
	return nil
}
