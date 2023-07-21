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

// Параметры для изменения конфигурации бота
var (
	intervalConfig = bot.IntervalStrategyConfig{
		PreferredPositionPrice:  200,
		MaxPositionPrice:        600,
		TopInstrumentsQuantity:  15,
		MinProfit:               0.3,
		DaysToCalculateInterval: 3,
		StopLossPercent:         1.8,
		AnalyseLowPercentile:    0,
		AnalyseHighPercentile:   0,
		Analyse:                 bot.BEST_WIDTH,
		// Параметры ниже не влияют на успех стратегии
		IntervalUpdateDelay:   time.Minute * MINUTES,
		SellOut:               true,
		StorageDBPath:         "examples/interval_bot/candles/candles.db",
		StorageCandleInterval: pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
		StorageFromTime:       time.Date(2023, 1, 10, 0, 0, 0, 0, time.Local),
		StorageUpdate:         true,
	}
	// Критерий для отбора бумаг. Акции, фонды, акции и фонды
	selection = SHARES
	// cancelAhead - Событие STOP для остановки будет отправлено в канал за cancelAhead до конца торгов
	cancelAhead = time.Minute * 5
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

// InstrumentsSelection - Типы инструментов для отбора
type InstrumentsSelection int

const (
	// SHARES - Акции
	SHARES InstrumentsSelection = iota
	// ETFS - Фонды
	ETFS
	// SHARES_AND_ETFS - Акции и фонды
	SHARES_AND_ETFS
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
	instrumentIds := make([]string, 0, INSTRUMENTS_MAX)
	insrtumentsService := client.NewInstrumentsServiceClient()
	// получаем список фондов доступных для торговли через investAPI
	etfsResp, err := insrtumentsService.Etfs(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// рублевые фонды с московской биржи
	etfs := etfsResp.GetInstruments()
	// получаем список акций доступных для торговли через investAPI
	sharesResp, err := insrtumentsService.Shares(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// рублевые акции c московской биржи
	shares := sharesResp.GetInstruments()
	// результаты отбора фондов и акций по бирже и валюте
	shareIds := make([]string, 0)
	etfsIds := make([]string, 0)

	for _, share := range shares {
		if len(shareIds) > INSTRUMENTS_MAX-1 {
			break
		}
		exchange := strings.EqualFold(share.GetExchange(), EXCHANGE)
		currency := strings.EqualFold(share.GetCurrency(), CURRENCY)
		if exchange && currency && !share.GetForQualInvestorFlag() {
			shareIds = append(shareIds, share.GetUid())
		}
	}

	for _, etf := range etfs {
		if len(etfsIds) > INSTRUMENTS_MAX-1 {
			break
		}
		exchange := strings.EqualFold(etf.GetExchange(), EXCHANGE)
		currency := strings.EqualFold(etf.GetCurrency(), CURRENCY)
		if exchange && currency {
			etfsIds = append(etfsIds, etf.GetUid())
		}
	}
	// заполняем instrumentIds в зависимости от выбранного selection
	switch selection {
	case SHARES:
		instrumentIds = shareIds
	case ETFS:
		instrumentIds = etfsIds
	case SHARES_AND_ETFS:
		instrumentIds = append(instrumentIds, shareIds...)
		instrumentIds = append(instrumentIds, etfsIds...)
	}
	logger.Infof("got %v instruments", len(instrumentIds))

	// передаем инструменты в конфиг
	intervalConfig.Instruments = instrumentIds

	// создание интервального бота
	intervalBot, err := bot.NewBot(ctx, client, intervalConfig)
	if err != nil {
		logger.Fatalf("interval bot creating fail %v", err.Error())
	}

	wg := &sync.WaitGroup{}
	// Таймер для Московской биржи, отслеживает расписание и дает сигналы, на остановку/запуск бота
	t := investgo.NewTimer(client, EXCHANGE, cancelAhead)

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
