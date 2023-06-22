package bot

import (
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
)

type Instrument struct {
	// quantity - Количество лотов, которое покупает/продает исполнитель за 1 поручение
	quantity int64
	// lot - Лотность инструмента
	lot int32
	// currency - Код валюты инструмента
	currency string
	// inStock - Флаг открытой позиции по инструменту, если true - позиция открыта
	inStock bool
	// entryPrice - После открытия позиции, сохраняется цена этой сделки
	entryPrice float64
}

// Executor - Вызывается ботом и исполняет торговые поручения
type Executor struct {
	// instruments - Инструменты, которыми торгует исполнитель
	instruments map[string]Instrument
	// minProfit - Процент минимального профита, после которого выставляются рыночные заявки
	minProfit float64

	// lastPrices - Мапа последних цен по инструментам, бот в нее пишет, исполнитель читает
	lastPrices map[string]float64

	client            *investgo.Client
	ordersService     *investgo.OrdersServiceClient
	operationsService *investgo.OperationsServiceClient
}

// NewExecutor - Создание экземпляра исполнителя
func NewExecutor(c *investgo.Client, ids map[string]Instrument, lp map[string]float64, minProfit float64) *Executor {
	return &Executor{
		instruments:       ids,
		lastPrices:        lp,
		client:            c,
		ordersService:     c.NewOrdersServiceClient(),
		operationsService: c.NewOperationsServiceClient(),
		minProfit:         minProfit,
	}
}

// Buy - Метод покупки инструмента с идентификатором id
func (e *Executor) Buy(id string) error {
	currentInstrument := e.instruments[id]
	// если этот инструмент уже куплен ботом
	if currentInstrument.inStock {
		return nil
	}
	// если не хватает средств для покупки
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
		currentInstrument.entryPrice = resp.GetExecutedOrderPrice().ToFloat()
	}
	e.instruments[id] = currentInstrument
	e.client.Logger.Infof("Buy with %v, price %v", resp.GetFigi(), resp.GetExecutedOrderPrice().ToFloat())
	return nil
}

// Sell - Метод покупки инструмента с идентификатором id
func (e *Executor) Sell(id string) (float64, error) {
	currentInstrument := e.instruments[id]
	if !currentInstrument.inStock {
		return 0, nil
	}
	if profitable := e.isProfitable(id); !profitable {
		return 0, nil
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
		return 0, err
	}
	var profit float64
	if resp.GetExecutionReportStatus() == pb.OrderExecutionReportStatus_EXECUTION_REPORT_STATUS_FILL {
		currentInstrument.inStock = false
		profit = resp.GetExecutedOrderPrice().ToFloat() - currentInstrument.entryPrice
	}
	e.client.Logger.Infof("Sell with %v, price %v", resp.GetFigi(), resp.GetExecutedOrderPrice().ToFloat())
	e.instruments[id] = currentInstrument
	return profit, nil
}

func (e *Executor) isProfitable(id string) bool {
	return ((e.lastPrices[id]-e.instruments[id].entryPrice)/e.instruments[id].entryPrice)*100 > e.minProfit
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

// SellOut - Метод выхода из всех текущих позиций
func (e *Executor) SellOut() error {
	resp, err := e.operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		return err
	}
	// TODO for futures and options
	securities := resp.GetSecurities()
	for _, security := range securities {
		var lot int64
		instrument, ok := e.instruments[security.GetInstrumentUid()]
		if !ok {
			// если бот не открывал эту позицию, он не будет ее закрывать
			e.client.Logger.Infof("%v not found in executor instruments map", security.GetInstrumentUid())
			continue
		} else {
			lot = int64(instrument.lot)
		}
		balanceInLots := security.GetBalance() / lot
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
