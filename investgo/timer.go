package investgo

import (
	"context"
	"errors"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"strings"
	"time"
)

// Event - события, START - сигнал к запуску, STOP - сигнал к остановке
type Event int

const (
	START Event = iota
	STOP
)

type Timer struct {
	client             *Client
	instrumentsService *InstrumentsServiceClient
	exchange           string
	lastEvent          Event
	cancel             context.CancelFunc
	ticker             chan struct{}
	events             chan Event
}

// NewTimer - Таймер сигнализирует о начале/завершении основной торговой сессии на конкретной бирже
func NewTimer(c *Client, exchange string) *Timer {
	return &Timer{
		client:             c,
		instrumentsService: c.NewInstrumentsServiceClient(),
		exchange:           exchange,
		ticker:             make(chan struct{}, 1),
		events:             make(chan Event, 1),
	}
}

// Events - Канал событий об открытии/закрытии торгов
func (t *Timer) Events() chan Event {
	return t.events
}

// Start - Запуск таймера
func (t *Timer) Start(ctx context.Context) error {
	defer t.shutdown()
	ctxTimer, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	//
	t.Wait(ctxTimer, 0)
	for {
		select {
		case <-ctxTimer.Done():
			return nil
		case <-t.ticker:
			// получаем текущее время
			from := time.Now()
			to := from.Add(time.Hour * 24)

			// получаем ближайшие два торговых дня
			resp, err := t.instrumentsService.TradingSchedules(t.exchange, from, to)
			if err != nil {
				return err
			}

			exchanges := resp.GetExchanges()
			days := make([]*pb.TradingDay, 0)
			for _, ex := range exchanges {
				if strings.EqualFold(ex.GetExchange(), t.exchange) {
					days = ex.GetDays()
				}
			}

			var today *pb.TradingDay
			if len(days) > 1 {
				today = days[0]
			}

			// если этот день оказался неторговым, то находим ближайший торговый и ждем до старта торгов в этот день
			if !today.GetIsTradingDay() {
				today, err = t.findTradingDay(from)
				if err != nil {
					return err
				}

			}
			switch {
			// если торги еще не начались
			case time.Now().Before(today.GetStartTime().AsTime()):
				t.Wait(ctxTimer, time.Until(today.GetStartTime().AsTime().Local()))
				t.events <- START
				t.Wait(ctxTimer, time.Until(today.GetEndTime().AsTime().Local()))
				t.events <- STOP
				// если сегодня торги уже идут
			case time.Now().After(today.GetStartTime().AsTime()) && time.Now().Before(today.GetEndTime().AsTime().Local()):
				t.client.Logger.Infof("now is trading time, sleep for %v", today.GetEndTime().AsTime().Local().String())
				t.events <- START
				t.Wait(ctxTimer, time.Until(today.GetEndTime().AsTime().Local()))
				t.events <- STOP
				// если на сегодня торги уже окончены
			case time.Now().After(today.GetEndTime().AsTime().Local()):
				// спать час, пока не дождемся следующего дня
				t.client.Logger.Infof("%v closed, wait next day", t.exchange)
				t.Wait(ctxTimer, time.Hour)
			}
		}
	}
}

// Stop - Завершение работы таймера
func (t *Timer) Stop() {
	t.cancel()
}

func (t *Timer) shutdown() {
	t.client.Logger.Infof("stop %v timer", t.exchange)
	close(t.events)
	close(t.ticker)
}

func (t *Timer) Wait(ctx context.Context, dur time.Duration) {
	tim := time.NewTimer(dur)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tim.C:
			// t.ticker <- struct{}{}
			return
		}
	}
}

// findTradingDay - Поиск ближайшего торгового дня
func (t *Timer) findTradingDay(start time.Time) (*pb.TradingDay, error) {
	resp, err := t.instrumentsService.TradingSchedules(t.exchange, start, start.Add(time.Hour*24*7))
	if err != nil {
		return nil, err
	}
	for _, ex := range resp.GetExchanges() {
		if strings.EqualFold(ex.GetExchange(), t.exchange) {
			for _, day := range ex.GetDays() {
				if day.GetIsTradingDay() {
					return day, nil
				}
			}
			// если не нашлось дня, запросим еще на неделю расписание
			return t.findTradingDay(start.Add(time.Hour * 24 * 7))
		}
	}
	return nil, errors.New("trading day not found")
}
