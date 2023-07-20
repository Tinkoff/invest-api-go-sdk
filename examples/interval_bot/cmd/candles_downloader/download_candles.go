package main

import (
	"context"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	// INSTRUMENTS_MAX - Максимальное кол-во инструментов
	INSTRUMENTS_MAX = 300
	// EXCHANGE - Биржа на которой будет работать бот
	EXCHANGE = "MOEX"
	// CURRENCY - Валюта для работы бота
	CURRENCY = "RUB"
	// DB_PATH - Путь к базе данных sqlite
	DB_PATH = "examples/interval_bot/candles/candles.db"
)

var (
	// FROM - Стартовое время для загрузки свечей
	FROM = time.Date(2023, 1, 10, 0, 0, 0, 0, time.Local)
	// INTERVAL - Интервал для запроса свечей
	INTERVAL = pb.CandleInterval_CANDLE_INTERVAL_1_MIN
)

func main() {
	// загружаем конфигурацию для сдк из .yaml файла
	sdkConfig, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("config loading error %v", err.Error())
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// сдк использует для внутреннего логирования investgo.Logger
	// для примера передадим uber.zap
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
	zapConfig.EncoderConfig.TimeKey = "time"
	l, err := zapConfig.Build()
	logger := l.Sugar()
	defer func() {
		err := logger.Sync()
		if err != nil {
			log.Printf(err.Error())
		}
	}()
	if err != nil {
		log.Fatalf("logger creating error %v", err)
	}
	// создаем клиента для investAPI, он позволяет создавать нужные сервисы и уже
	// через них вызывать нужные методы
	client, err := investgo.NewClient(ctx, sdkConfig, logger)
	if err != nil {
		logger.Fatalf("client creating error %v", err.Error())
	}
	defer func() {
		logger.Infof("closing client connection")
		err := client.Stop()
		if err != nil {
			logger.Errorf("client shutdown error %v", err.Error())
		}
	}()

	// для создания стратеги нужно ее сконфигурировать, для этого получим список идентификаторов инструментов,
	// которыми предстоит торговать
	insrtumentsService := client.NewInstrumentsServiceClient()
	// получаем список акций доступных для торговли через investAPI
	instrumentsResp, err := insrtumentsService.Shares(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// слайс идентификаторов торговых инструментов instrument_uid
	// акции с московской биржи
	instrumentIds := make([]string, 0, 300)
	shares := instrumentsResp.GetInstruments()
	for _, share := range shares {
		if len(instrumentIds) > INSTRUMENTS_MAX-1 {
			break
		}
		exchange := strings.EqualFold(share.GetExchange(), EXCHANGE)
		currency := strings.EqualFold(share.GetCurrency(), CURRENCY)
		if exchange && currency && !share.GetForQualInvestorFlag() {
			instrumentIds = append(instrumentIds, share.GetUid())
		}
	}
	logger.Infof("got %v instruments", len(instrumentIds))
	// инициализируем sqlite для сохранения исторических свечей по инструментам
	db, err := initDB(DB_PATH)
	if err != nil {
		logger.Fatalf(err.Error())
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Errorf(err.Error())
		}
	}()

	// для каждого инструмента запрашиваем свечи и сохраняем в бд
	mds := client.NewMarketDataServiceClient()
	now := time.Now()
	for i, id := range instrumentIds {
		candles, err := mds.GetHistoricCandles(&investgo.GetHistoricCandlesRequest{
			Instrument: id,
			Interval:   INTERVAL,
			From:       FROM,
			To:         now,
			File:       false,
			FileName:   "",
		})

		logger.Infof("got %v candles for %v", len(candles), id)

		err = storeCandlesInDB(db, id, now, candles)
		if err != nil {
			logger.Errorf(err.Error())
		}
		logger.Infof("store in db complete candle %v/%v", i+1, len(instrumentIds))
	}
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

create table if not exists updates (
  instrument_id text unique,
  time integer
);
`

// initDB - Инициализация бд
func initDB(path string) (*sqlx.DB, error) {
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
	log.Printf("database initialized")
	return db, nil
}

// storeCandlesInDB - Сохранение исторических свечей инструмента в бд
func storeCandlesInDB(db *sqlx.DB, uid string, update time.Time, hc []*pb.HistoricCandle) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

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

	if err := tx.Commit(); err != nil {
		return err
	}

	// записываем в базу время последнего обновления
	_, err = db.Exec(`insert or replace into updates(instrument_id, time) values (?, ?)`, uid, update.Unix())
	if err != nil {
		return err
	}
	log.Printf("%v %v candles uploaded in storage", uid, len(hc))
	return nil
}
