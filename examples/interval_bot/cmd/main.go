package main

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/examples/interval_bot/internal/bot"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
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
	// MINUTES - Интервал обновления исторических свечей для расчета нового коридора цен в минутах
	MINUTES = 5
)

func main() {
	// загружаем конфигурацию для сдк из .yaml файла
	sdkConfig, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("config loading error %v", err.Error())
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
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
	// слайс идентификаторов торговых инструментов instrument_uid
	instrumentIds := make([]string, 0, 300)
	insrtumentsService := client.NewInstrumentsServiceClient()
	// получаем список акций доступных для торговли через investAPI
	instrumentsResp, err := insrtumentsService.Shares(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// слайс идентификаторов торговых инструментов instrument_uid
	// акции с московской биржи
	shares := instrumentsResp.GetInstruments()
	for _, share := range shares {
		if len(instrumentIds) > INSTRUMENTS_MAX-1 {
			break
		}
		exchange := strings.EqualFold(share.GetExchange(), EXCHANGE)
		currency := strings.EqualFold(share.GetCurrency(), CURRENCY)
		if exchange && currency && !share.GetForQualInvestorFlag() && share.GetUid() != "7c9454d0-af4a-4380-82d5-394ee7c9b037" {
			instrumentIds = append(instrumentIds, share.GetUid())
		}
	}
	logger.Infof("got %v instruments", len(instrumentIds))

	intervalConfig := bot.IntervalStrategyConfig{
		Instruments:             instrumentIds,
		PreferredPositionPrice:  200,
		MaxPositionPrice:        600,
		MinProfit:               0.3,
		IntervalUpdateDelay:     time.Minute * MINUTES,
		TopInstrumentsQuantity:  15,
		SellOut:                 true,
		StorageDBPath:           "examples/interval_bot/candles/candles.db",
		StorageCandleInterval:   pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
		StorageFromTime:         time.Date(2023, 1, 10, 0, 0, 0, 0, time.Local),
		StorageUpdate:           true,
		DaysToCalculateInterval: 3,
		StopLossPercent:         1.8,
		AnalyseLowPercentile:    0,
		AnalyseHighPercentile:   0,
	}
	// создание интервального бота
	intervalBot, err := bot.NewBot(ctx, client, intervalConfig)
	if err != nil {
		logger.Fatalf("interval bot creating fail %v", err.Error())
	}

	wg := &sync.WaitGroup{}
	// Таймер для Московской биржи, отслеживает расписание и дает сигналы, на остановку/запуск бота
	// cancelAhead - Событие STOP будет отправлено в канал за cancelAhead до конца торгов
	cancelAhead := time.Minute * 5
	t := investgo.NewTimer(client, "MOEX", cancelAhead)

	// запуск таймера
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		err := t.Start(ctx)
		if err != nil {
			logger.Errorf(err.Error())
		}
	}(ctx)

	// по сигналам останавливаем таймер
	go func() {
		<-sigs
		t.Stop()
	}()

	// чтение событий от таймера и управление ботом
	events := t.Events()
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				logger.Infof("got event = %v", ev)
				switch ev {
				case investgo.START:
					// запуск бота
					err = intervalBot.Run()
					if err != nil {
						logger.Fatalf(err.Error())
					}
				case investgo.STOP:
					// остановка бота
					err = intervalBot.Stop()
					if err != nil {
						logger.Errorf(err.Error())
					}
				}
			}
		}
	}(ctx)

	wg.Wait()
}
