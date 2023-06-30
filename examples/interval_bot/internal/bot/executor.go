package bot

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"math"
	"sync"
	"time"
)

type InstrumentState int

const (
	INSTRUMENT_IN_STOCK InstrumentState = iota
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

	state InstrumentState

	priceStep float64
	// entryPrice - После открытия позиции, сохраняется цена этой сделки
	entryPrice float64
}

// Executor - Вызывается ботом и исполняет торговые поручения
type Executor struct {
	// instruments - Инструменты, которыми торгует исполнитель
	instruments map[string]Instrument
	// minProfit - Процент профита, с которым выставляются лимитные заявки
	profit float64

	wg     *sync.WaitGroup
	cancel context.CancelFunc

	// lastPrices - Текущие позиции на счете, обновляются через стрим сервиса операций
	positions *Positions

	client            *investgo.Client
	ordersService     *investgo.OrdersServiceClient
	operationsService *investgo.OperationsServiceClient
}

func NewExecutor(ctx context.Context, c *investgo.Client, ids map[string]Instrument, minProfit float64) *Executor {
	ctxExecutor, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}

	e := &Executor{
		instruments:       ids,
		positions:         NewPositions(),
		wg:                wg,
		cancel:            cancel,
		client:            c,
		ordersService:     c.NewOrdersServiceClient(),
		operationsService: c.NewOperationsServiceClient(),
	}
	// Сразу запускаем исполнителя из его же конструктора
	e.start(ctxExecutor)
	return e
}

func (e *Executor) BuyLimit(id string, price float64) error {
	currentInstrument, ok := e.instruments[id]
	if !ok {
		return fmt.Errorf("instrument %v not found in executor map", id)
	}
	resp, err := e.ordersService.Buy(&investgo.PostOrderRequestShort{
		InstrumentId: id,
		Quantity:     0,
		Price:        floatToQuotation(price, currentInstrument.priceStep),
		AccountId:    e.client.Config.AccountId,
		OrderType:    pb.OrderType_ORDER_TYPE_LIMIT,
		OrderId:      investgo.CreateUid(),
	})
	if err != nil {
		return err
	}
	resp.GetOrderId()
	return nil
}

func (e *Executor) SellLimit(id string, price float64) error {
	return nil
}

// floatToQuotation - Перевод float в Quotation. Если шаг цены либо < 1, либо целый. Округление вниз
func floatToQuotation(number float64, step float64) *pb.Quotation {
	if step < 1 {
		units, frac := math.Modf(number)
		intPres := int64(step * math.Pow(10, 9))

		intFract := int64(frac * math.Pow(10, 9))
		k := intFract / intPres
		return &pb.Quotation{
			Units: int64(units),
			Nano:  int32(k * intPres),
		}
	} else {
		k := int64(number) / int64(step)
		return &pb.Quotation{
			Units: k * int64(step),
			Nano:  0,
		}
	}
}

// Positions - Данные о позициях счета
type Positions struct {
	mx sync.Mutex
	pd *pb.PositionData
}

func NewPositions() *Positions {
	return &Positions{
		pd: &pb.PositionData{},
	}
}

// Update - Обновление позиций
func (p *Positions) Update(data *pb.PositionData) {
	p.mx.Lock()
	p.pd = data
	p.mx.Unlock()
}

// Get - получение позиций
func (p *Positions) Get() *pb.PositionData {
	p.mx.Lock()
	defer p.mx.Unlock()
	return p.pd
}

// start - Запуск чтения стримов позиций и последних цен
func (e *Executor) start(ctx context.Context) {
	e.wg.Add(1)
	go func(ctx context.Context) {
		defer e.wg.Done()
		err := e.listenPositions(ctx)
		if err != nil {
			e.client.Logger.Errorf(err.Error())
		}
	}(ctx)
}

// listenPositions - Метод слушает стрим позиций и обновляет их
func (e *Executor) listenPositions(ctx context.Context) error {
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

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		err := stream.Listen()
		if err != nil {
			e.client.Logger.Errorf(err.Error())
		}
	}()

	e.wg.Add(1)
	go func(ctx context.Context) {
		defer e.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-positionsChan:
				if !ok {
					return
				}
				// e.client.Logger.Infof("update from positions stream %v\n", p.GetMoney())
				e.positions.Update(p)
			}
		}
	}(ctx)

	<-ctx.Done()
	e.client.Logger.Infof("stop updating positions in executor")
	stream.Stop()
	return nil
}

// updatePositionsUnary - Unary метод обновления позиций
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
