package bot

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"sync"
	"time"
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

type Positions struct {
	mx sync.Mutex
	pd *pb.PositionData
}

func NewPositions() *Positions {
	return &Positions{pd: &pb.PositionData{}}
}

func (p *Positions) Update(data *pb.PositionData) {
	p.mx.Lock()
	p.pd = data
	p.mx.Unlock()
}

func (p *Positions) Get() *pb.PositionData {
	p.mx.Lock()
	defer p.mx.Unlock()
	return p.pd
}

// Executor - Вызывается ботом и исполняет торговые поручения
type Executor struct {
	// instruments - Инструменты, которыми торгует исполнитель
	instruments map[string]Instrument
	// minProfit - Процент минимального профита, после которого выставляются рыночные заявки
	minProfit float64

	// lastPrices - Мапа последних цен по инструментам, бот в нее пишет, исполнитель читает
	lastPrices map[string]float64
	positions  *Positions

	client            *investgo.Client
	ordersService     *investgo.OrdersServiceClient
	operationsService *investgo.OperationsServiceClient
}

// NewExecutor - Создание экземпляра исполнителя
func NewExecutor(ctx context.Context, c *investgo.Client, ids map[string]Instrument, minProfit float64) *Executor {
	e := &Executor{
		instruments:       ids,
		lastPrices:        make(map[string]float64, len(ids)),
		client:            c,
		ordersService:     c.NewOrdersServiceClient(),
		operationsService: c.NewOperationsServiceClient(),
		minProfit:         minProfit,
		positions:         NewPositions(),
	}

	go func(ctx context.Context) {
		err := e.updatePositions(ctx)
		if err != nil {
			e.client.Logger.Errorf(err.Error())
		}
	}(ctx)

	return e
}

func (e *Executor) updatePositionsUnary() error {
	resp, err := e.operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		return err
	}
	// два слайса *MoneyValue
	available := resp.GetMoney()
	blocked := resp.GetBlocked()

	// слайс *PositionMoney
	positionMoney := make([]*pb.PositionsMoney, 0)
	// ключ - код валюты, значение - *PositionMoney
	moneyByCurrency := make(map[string]*pb.PositionsMoney, 0)

	for _, avail := range available {
		moneyByCurrency[avail.GetCurrency()] = &pb.PositionsMoney{
			AvailableValue: avail,
			BlockedValue:   nil,
		}
	}

	for _, block := range blocked {
		m := moneyByCurrency[block.GetCurrency()]
		moneyByCurrency[block.GetCurrency()] = &pb.PositionsMoney{
			AvailableValue: m.GetAvailableValue(),
			BlockedValue:   block,
		}
	}

	for _, money := range moneyByCurrency {
		positionMoney = append(positionMoney, money)
	}

	// обновляем позиции для исполнителя
	e.positions.Update(&pb.PositionData{
		AccountId:  e.client.Config.AccountId,
		Money:      positionMoney,
		Securities: resp.GetSecurities(),
		Futures:    resp.GetFutures(),
		Options:    resp.GetOptions(),
		Date:       investgo.TimeToTimestamp(time.Now()),
	})

	return nil
}

func (e *Executor) updatePositions(ctx context.Context) error {
	err := e.updatePositionsUnary()
	if err != nil {
		return err
	}
	operationsStreamService := e.client.NewOperationsStreamClient()
	stream, err := operationsStreamService.PositionsStream([]string{e.client.Config.AccountId})
	if err != nil {
		return err
	}
	positionsChan := stream.Positions()

	go func() {
		err := stream.Listen()
		if err != nil {
			e.client.Logger.Errorf(err.Error())
		}
	}()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-positionsChan:
				if !ok {
					return
				}
				e.client.Logger.Infof("update from positions stream %v\n", p.GetMoney())
				e.positions.Update(p)
			}
		}
	}(ctx)

	<-ctx.Done()
	e.client.Logger.Infof("stop updating positions in executor")
	stream.Stop()
	return nil
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
		// разница в цене инструмента * лотность * кол-во лотов
		profit = (resp.GetExecutedOrderPrice().ToFloat() - currentInstrument.entryPrice) * float64(currentInstrument.lot) * float64(currentInstrument.quantity)
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
	positionMoney := e.positions.Get().GetMoney()
	var moneyInFloat float64
	for _, pm := range positionMoney {
		m := pm.GetAvailableValue()
		if m.GetCurrency() == e.instruments[id].currency {
			moneyInFloat = m.ToFloat()
		}
	}

	// TODO убрать, когда починят стрим
	if moneyInFloat < 0 {
		e.client.Logger.Infof("balance < 0, update positions by unary call")
		err := e.updatePositionsUnary()
		if err != nil {
			e.client.Logger.Errorf(err.Error())
		}
		return e.possibleToBuy(id)
	}

	// TODO сравнение дробных чисел
	if moneyInFloat < required {
		e.client.Logger.Infof("executor: not enough money to buy order")
	}
	return moneyInFloat > required
}

func (e *Executor) possibleToSell() {

}

// SellOut - Метод выхода из всех текущих позиций
func (e *Executor) SellOut() error {
	// TODO for futures and options
	resp, err := e.operationsService.GetPositions(e.client.Config.AccountId)
	if err != nil {
		return err
	}

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
