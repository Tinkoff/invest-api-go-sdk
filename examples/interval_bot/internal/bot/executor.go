package bot

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"math"
	"sync"
)

// Executor - Вызывается ботом и исполняет торговые поручения
type Executor struct {
	// instruments - Инструменты, которыми торгует исполнитель
	instruments map[string]Instrument
	// minProfit - Процент профита, с которым выставляются лимитные заявки
	profit float64

	wg     *sync.WaitGroup
	cancel context.CancelFunc

	client            *investgo.Client
	ordersService     *investgo.OrdersServiceClient
	operationsService *investgo.OperationsServiceClient
}

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

// floatToQuotation - Перевод float в Quotation. Если шаг цены либо < 1, либо целый. Окгругление вниз
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
