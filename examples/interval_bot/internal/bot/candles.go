package bot

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"log"
	"time"
)

// StorageInstrument - Информация об инструменте в хранилище
type StorageInstrument struct {
	CandleInterval pb.CandleInterval
	PriceStep      *pb.Quotation
	LastUpdate     time.Time
}

// CandlesStorage - Локально хранилище свечей в sqlite
type CandlesStorage struct {
	instruments map[string]StorageInstrument
	candles     map[string][]*pb.HistoricCandle
	mds         *investgo.MarketDataServiceClient
	logger      investgo.Logger
	db          *sqlx.DB
}

// NewCandlesStorage - Создание хранилища свечей
func NewCandlesStorage(dbpath string, update bool, required map[string]StorageInstrument, l investgo.Logger, mds *investgo.MarketDataServiceClient) (*CandlesStorage, error) {
	cs := &CandlesStorage{
		mds:         mds,
		instruments: make(map[string]StorageInstrument),
		candles:     make(map[string][]*pb.HistoricCandle),
		logger:      l,
	}
	// инициализируем бд
	db, err := cs.initDB(dbpath)
	if err != nil {
		return nil, err
	}
	cs.db = db
	// получаем инструменты, которые уже есть в бд
	unique, err := cs.uniqueInstruments()
	if err != nil {
		return nil, err
	}
	// если инструмента в бд нет, то загружаем данные по нему
	for id, candles := range required {
		if _, ok := unique[id]; !ok {
			now := time.Now()
			newCandles, err := cs.mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
				Instrument: id,
				Interval:   candles.CandleInterval,
				From:       candles.LastUpdate,
				To:         now,
				File:       false,
				FileName:   "",
			})
			if err != nil {
				return nil, err
			}
			candles.LastUpdate = now
			// обновляем значение последнего запроса
			cs.instruments[id] = candles
			err = cs.storeCandlesInDB(id, newCandles)
		} else {
			cs.instruments[id] = candles
		}
	}
	// вычитываем из бд даты последних обновлений
	if update {
		err = cs.lastUpdates()
		// обновляем в бд данные по всем инструментам
		for id := range required {
			err = cs.UpdateCandlesHistory(id)
			if err != nil {
				return nil, err
			}
		}
	}
	// загрузка всех свечей из бд в мапу
	for id := range required {
		tmp, err := cs.CandlesAll(id)
		if err != nil {
			return nil, err
		}
		cs.candles[id] = tmp
	}

	return cs, err
}

// Close - Закрытие хранилища свечей
func (c *CandlesStorage) Close() error {
	return c.db.Close()
}

// Candles - Получение исторических свечей по uid инструмента
//func (c *CandlesStorage) Candles(id string, from, to time.Time) ([]*pb.HistoricCandle, error) {
//	instrument, ok := c.instruments[id]
//	if !ok {
//		return nil, fmt.Errorf("%v instrument not found, at first LoadCandlesHistory()", id)
//	}
//	return c.loadCandlesFromDB(id, instrument.PriceStep, from, to)
//}

// Candles - Получение исторических свечей по uid инструмента
func (c *CandlesStorage) Candles(id string, from, to time.Time) ([]*pb.HistoricCandle, error) {
	allCandles, ok := c.candles[id]
	if !ok {
		return nil, fmt.Errorf("%v instrument not found, at first LoadCandlesHistory()", id)
	}
	indexes := [2]int{}
	times := [2]time.Time{from, to}
	currIndex := 0
	for i, candle := range allCandles {
		if currIndex < 2 {
			if candle.GetTime().AsTime().After(times[currIndex]) {
				indexes[currIndex] = i
				currIndex++
			}
		} else {
			break
		}
	}
	return allCandles[indexes[0]:indexes[1]], nil
}

// CandlesAll - Получение всех исторических свечей из хранилища по uid инструмента
func (c *CandlesStorage) CandlesAll(uid string) ([]*pb.HistoricCandle, error) {
	instrument, ok := c.instruments[uid]
	if !ok {
		return nil, fmt.Errorf("%v instrument not found, at first LoadCandlesHistory()", uid)
	}

	stmt, err := c.db.Preparex(`select * from candles where instrument_uid=?`)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := stmt.Close()
		if err != nil {
			c.logger.Errorf(err.Error())
		}
	}()

	rows, err := stmt.Queryx(uid)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			c.logger.Errorf(err.Error())
		}
	}()

	dst := &CandleDB{}
	candles := make([]*pb.HistoricCandle, 0)
	for rows.Next() {
		err = rows.StructScan(dst)
		if err != nil {
			return nil, err
		}
		if dst != nil {
			candles = append(candles, &pb.HistoricCandle{
				Open:       investgo.FloatToQuotation(dst.Open, instrument.PriceStep),
				High:       investgo.FloatToQuotation(dst.High, instrument.PriceStep),
				Low:        investgo.FloatToQuotation(dst.Low, instrument.PriceStep),
				Close:      investgo.FloatToQuotation(dst.Close, instrument.PriceStep),
				Volume:     int64(dst.Volume),
				Time:       investgo.TimeToTimestamp(time.Unix(dst.Time, 0)),
				IsComplete: dst.IsComplete == 1,
			})
		}
	}
	c.logger.Infof("%v %v candles downloaded from storage", uid, len(candles))

	return candles, nil
}

// LoadCandlesHistory - Начальная загрузка исторических свечей для нового инструмента (from - now)
func (c *CandlesStorage) LoadCandlesHistory(id string, interval pb.CandleInterval, inc *pb.Quotation, from time.Time) error {
	now := time.Now()
	newCandles, err := c.mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
		Instrument: id,
		Interval:   interval,
		From:       from,
		To:         now,
		File:       false,
		FileName:   "",
	})
	if err != nil {
		return err
	}
	c.instruments[id] = StorageInstrument{
		CandleInterval: interval,
		PriceStep:      inc,
		LastUpdate:     now,
	}
	return c.storeCandlesInDB(id, newCandles)
}

// UpdateCandlesHistory - Загрузить исторические свечи от времени последнего обновления до now
func (c *CandlesStorage) UpdateCandlesHistory(id string) error {
	c.logger.Infof("%v candles updating...", id)
	instrument, ok := c.instruments[id]
	if !ok {
		return fmt.Errorf("%v not found in candles storage", id)
	}
	now := time.Now()
	newCandles, err := c.mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
		Instrument: id,
		Interval:   instrument.CandleInterval,
		From:       instrument.LastUpdate,
		To:         now,
		File:       false,
		FileName:   "",
	})
	if err != nil {
		return err
	}
	instrument.LastUpdate = now
	c.instruments[id] = instrument
	return c.storeCandlesInDB(id, newCandles)
}

// lastUpdates - Обновление времени последнего обновления свечей по инструментам в мапе Instruments
func (c *CandlesStorage) lastUpdates() error {
	c.logger.Infof("update lastUpdate time from storage...")
	var lastUpdUnix int64
	for id, candles := range c.instruments {
		err := c.db.Get(&lastUpdUnix, `select max(time) from candles where instrument_uid=?`, id)
		if err != nil {
			return err
		}
		candles.LastUpdate = time.Unix(lastUpdUnix, 0)
		c.instruments[id] = candles
	}
	return nil
}

// uniqueInstruments - Метод возвращает мапу с уникальными значениями uid инструментов в бд
func (c *CandlesStorage) uniqueInstruments() (map[string]struct{}, error) {
	instruments := make([]string, 0)
	err := c.db.Select(&instruments, `select distinct instrument_uid from candles`)
	if err != nil {
		return nil, err
	}
	m := make(map[string]struct{})
	for _, instrument := range instruments {
		m[instrument] = struct{}{}
	}
	c.logger.Infof("got %v unique instruments from storage", len(m))
	return m, nil
}

var schema = `
create table if not exists candles (
    id integer primary key autoincrement,
    instrument_uid text,
	open real,
	close real,
	high real,
	low real,
	volume integer,
	time integer,
	is_complete integer
);
`

// initDB - Инициализация бд
func (c *CandlesStorage) initDB(path string) (*sqlx.DB, error) {
	db, err := sqlx.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	c.logger.Infof("database initialized")
	return db, nil
}

// storeCandlesInDB - Сохранение исторических свечей инструмента в бд
func (c *CandlesStorage) storeCandlesInDB(uid string, hc []*pb.HistoricCandle) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Commit(); err != nil {
			log.Printf(err.Error())
		}
	}()

	insertCandle, err := tx.Prepare(`insert into candles (instrument_uid, open, close, high, low, volume, time, is_complete) 
		values (?, ?, ?, ?, ?, ?, ?, ?) `)
	if err != nil {
		return err
	}
	defer func() {
		if err := insertCandle.Close(); err != nil {
			log.Printf(err.Error())
		}
	}()

	for _, candle := range hc {
		_, err := insertCandle.Exec(uid,
			candle.GetOpen().ToFloat(),
			candle.GetClose().ToFloat(),
			candle.GetHigh().ToFloat(),
			candle.GetLow().ToFloat(),
			candle.GetVolume(),
			candle.GetTime().AsTime().Unix(),
			candle.GetIsComplete())
		if err != nil {
			return err
		}
	}
	c.logger.Infof("%v %v candles uploaded in storage", uid, len(hc))

	return nil
}

type CandleDB struct {
	Id            int     `db:"id"`
	InstrumentUid string  `db:"instrument_uid"`
	Open          float64 `db:"open"`
	Close         float64 `db:"close"`
	High          float64 `db:"high"`
	Low           float64 `db:"low"`
	Volume        int     `db:"volume"`
	Time          int64   `db:"time"`
	IsComplete    int     `db:"is_complete"`
}

// loadCandlesFromDB - Загрузка исторических свечей по инструменту из бд
func (c *CandlesStorage) loadCandlesFromDB(uid string, inc *pb.Quotation, from, to time.Time) ([]*pb.HistoricCandle, error) {
	stmt, err := c.db.Preparex(`select * from candles where instrument_uid=? and time between ? and ?`)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := stmt.Close()
		if err != nil {
			c.logger.Errorf(err.Error())
		}
	}()

	rows, err := stmt.Queryx(uid, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			c.logger.Errorf(err.Error())
		}
	}()

	dst := &CandleDB{}
	candles := make([]*pb.HistoricCandle, 0)
	for rows.Next() {
		err = rows.StructScan(dst)
		if err != nil {
			return nil, err
		}
		if dst != nil {
			candles = append(candles, &pb.HistoricCandle{
				Open:       investgo.FloatToQuotation(dst.Open, inc),
				High:       investgo.FloatToQuotation(dst.High, inc),
				Low:        investgo.FloatToQuotation(dst.Low, inc),
				Close:      investgo.FloatToQuotation(dst.Close, inc),
				Volume:     int64(dst.Volume),
				Time:       investgo.TimeToTimestamp(time.Unix(int64(dst.Time), 0)),
				IsComplete: dst.IsComplete == 1,
			})
		}
	}
	c.logger.Infof("%v %v candles downloaded from storage", uid, len(candles))
	return candles, nil
}
