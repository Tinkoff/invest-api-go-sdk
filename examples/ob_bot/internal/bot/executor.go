package bot

import (
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
)

const MIN_PROFIT = 1

type Instrument struct {
	quantity int64
	inStock  bool
	buyPrice float64
}

type Executor struct {
	instruments map[string]Instrument

	// lastPrices - read only for executor
	lastPrices map[string]float64

	client        *investgo.Client
	ordersService *investgo.OrdersServiceClient
}

func NewExecutor(c *investgo.Client, ids map[string]Instrument, lp map[string]float64) *Executor {
	return &Executor{
		instruments:   ids,
		lastPrices:    lp,
		client:        c,
		ordersService: c.NewOrdersServiceClient(),
	}
}

func (e *Executor) Buy(id string) error {
	currentInstrument := e.instruments[id]
	if currentInstrument.inStock {
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

func (e *Executor) possibleToBuy() {

}

func (e *Executor) possibleToSell() {

}

// SellOut - продать все текущие позиции
func (e *Executor) SellOut() error {
	operationsService := e.client.NewOperationsServiceClient()
	resp, err := operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		return err
	}
	instrumentsService := e.client.NewInstrumentsServiceClient()
	// TODO for futures and options
	securities := resp.GetSecurities()
	for _, security := range securities {
		balance := security.GetBalance()
		lot, err := instrumentsService.LotByUid(security.GetInstrumentUid())
		if err != nil {
			return err
		}
		balanceInLots := balance / lot
		if balance < 0 {
			resp, err := e.ordersService.Buy(&investgo.PostOrderRequestShort{
				InstrumentId: security.GetInstrumentUid(),
				Quantity:     -balanceInLots,
				Price:        nil,
				AccountId:    e.client.Config.AccountId,
				OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
				OrderId:      investgo.CreateUid(),
			})
			if err != nil {
				fmt.Println(investgo.MessageFromHeader(resp.GetHeader()))
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
				fmt.Println(investgo.MessageFromHeader(resp.GetHeader()))
				return err
			}
		}

	}
	return nil
}
