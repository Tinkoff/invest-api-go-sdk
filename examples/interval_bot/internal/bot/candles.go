package bot

import (
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"time"
)

type Candles struct {
	hc             []*pb.HistoricCandle
	candleInterval pb.CandleInterval
	lastUpdate     time.Time
}

// CandleInterval - Получение интервала исторических свечей
func (c *Candles) CandleInterval() pb.CandleInterval {
	if c != nil {
		return c.candleInterval
	}
	return pb.CandleInterval_CANDLE_INTERVAL_UNSPECIFIED
}

// HistoricCandle - Получение исторических свечей
func (c *Candles) HistoricCandle() []*pb.HistoricCandle {
	if c != nil {
		return c.hc
	}
	return nil
}

// AppendHistoricCandle - Добавление исторических свечей
func (c *Candles) AppendHistoricCandle(newCandles []*pb.HistoricCandle) error {
	if c != nil {
		c.hc = append(c.hc, newCandles...)
		return nil
	}
	return fmt.Errorf("candles is empty")
}

// LastUpdate - Получение времени последнего обновления свечей
func (c *Candles) LastUpdate() time.Time {
	if c != nil {
		return c.lastUpdate
	}
	return time.Time{}
}

type CandlesStorage struct {
	candles map[string]*Candles
	mds     *investgo.MarketDataServiceClient
}

func NewCandlesStorage(mds *investgo.MarketDataServiceClient) *CandlesStorage {
	return &CandlesStorage{
		candles: make(map[string]*Candles),
		mds:     mds,
	}
}

// Candles - Получение исторических свечей из хранилища
func (c *CandlesStorage) Candles(id string) ([]*pb.HistoricCandle, error) {
	candles, ok := c.candles[id]
	if !ok {
		return nil, fmt.Errorf("%v candles not found, at first LoadCandlesHistory()", id)
	}
	return candles.HistoricCandle(), nil
}

// UpdateCandlesHistory - Загрузить от времени последнего обновления до сейчас
func (c *CandlesStorage) UpdateCandlesHistory(id string) error {
	candles, ok := c.candles[id]
	if !ok {
		return fmt.Errorf("%v not found in candles storage", id)
	}
	now := time.Now()
	newCandles, err := c.mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
		Instrument: id,
		Interval:   candles.CandleInterval(),
		From:       candles.LastUpdate(),
		To:         now,
		File:       false,
		FileName:   "",
	})
	if err != nil {
		return err
	}
	err = candles.AppendHistoricCandle(newCandles)
	if err != nil {
		return err
	}
	candles.lastUpdate = now
	return nil
}

// LoadCandlesHistory - Начальная загрузка истории свечей
func (c *CandlesStorage) LoadCandlesHistory(ids []string, i pb.CandleInterval, from, to time.Time) error {
	for _, instrument := range ids {
		candles, err := c.mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
			Instrument: instrument,
			Interval:   i,
			From:       from,
			To:         to,
			File:       false,
			FileName:   "",
		})
		if err != nil {
			return err
		}
		c.candles[instrument] = &Candles{
			hc:             candles,
			lastUpdate:     to,
			candleInterval: i,
		}
	}
	return nil
}
