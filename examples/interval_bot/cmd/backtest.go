package main

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/examples/interval_bot/internal/bot"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"os/signal"
	"sort"
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
	// MINUTES - Интервал обновления исторических свечей для расчета нового коридора цен в минутах
	MINUTES = 10
)

// Report - отчет для бектеста на разных конфигах
type Report struct {
	bc                      bot.BacktestConfig
	totalProfit             float64
	averageDayPercentProfit float64
}

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
	// слайс идентификаторов торговых инструментов instrument_uid
	instrumentIds := make([]string, 0, 300)
	// instrumentIds := []string{"9654c2dd-6993-427e-80fa-04e80a1cf4da"}

	insrtumentsService := client.NewInstrumentsServiceClient()

	// получаем список фондов доступных для торговли через investAPI
	etfsResp, err := insrtumentsService.Etfs(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// рублевые фонды с московской биржи
	etfs := etfsResp.GetInstruments()
	for _, etf := range etfs {
		if len(instrumentIds) > INSTRUMENTS_MAX-1 {
			break
		}
		exchange := strings.EqualFold(etf.GetExchange(), EXCHANGE)
		currency := strings.EqualFold(etf.GetCurrency(), CURRENCY)
		if exchange && currency {
			instrumentIds = append(instrumentIds, etf.GetUid())
		}
	}

	//// получаем список акций доступных для торговли через investAPI
	//sharesResp, err := insrtumentsService.Shares(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	//if err != nil {
	//	logger.Errorf(err.Error())
	//}
	//// рублевые акции c московской биржи
	//shares := sharesResp.GetInstruments()
	//for _, share := range shares {
	//	if len(instrumentIds) > INSTRUMENTS_MAX-1 {
	//		break
	//	}
	//	exchange := strings.EqualFold(share.GetExchange(), EXCHANGE)
	//	currency := strings.EqualFold(share.GetCurrency(), CURRENCY)
	//	if exchange && currency {
	//		instrumentIds = append(instrumentIds, share.GetUid())
	//	}
	//}

	logger.Infof("got %v instruments", len(instrumentIds))

	intervalConfig := bot.IntervalStrategyConfig{
		Instruments:             instrumentIds,
		PreferredPositionPrice:  1000,
		MaxPositionPrice:        5000,
		MinProfit:               0.2,
		TopInstrumentsQuantity:  5,
		SellOut:                 true,
		StorageDBPath:           "examples/interval_bot/candles/candles.db",
		StorageCandleInterval:   pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
		StorageFromTime:         time.Date(2023, 1, 10, 0, 0, 0, 0, time.Local),
		StorageUpdate:           false,
		DaysToCalculateInterval: 2,
		StopLossPercent:         1,
		AnalyseLowPercentile:    40,
		AnalyseHighPercentile:   60,
	}
	// создание интервального бота
	intervalBot, err := bot.NewBot(ctx, client, intervalConfig)
	if err != nil {
		logger.Fatalf("interval bot creating fail %v", err.Error())
	}
	// интервал для проверки
	initDate := time.Date(2023, 1, 22, 0, 0, 0, 0, time.Local)
	stopDate := time.Date(2023, 7, 7, 0, 0, 0, 0, time.Local)

	//TestWithConfig(intervalBot, logger, initDate, stopDate, bot.BacktestConfig{
	//	Analyse:                 bot.MinProfit,
	//	LowPercentile:           0,
	//	HighPercentile:          0,
	//	MinProfit:               0.2,
	//	DaysToCalculateInterval: 1,
	//	StopLoss:                2,
	//	Commission:              0.05,
	//})

	TestWithMultipleConfigs(intervalBot, logger, initDate, stopDate)
}

func TestWithConfig(b *bot.Bot, logger investgo.Logger, start, stop time.Time, config bot.BacktestConfig) {
	date := start
	var totalPercentage, totalProfit float64

	var tradingDays int
	for date.Before(stop) {
		profit, percentage, err := b.BackTest(date, config)
		if err != nil {
			logger.Errorf(err.Error())
		}
		logger.Infof("day profit = %.9f", profit)
		logger.Infof("day profit percent = %.9f", percentage)
		totalPercentage += percentage
		totalProfit += profit
		if totalProfit != 0 {
			tradingDays++
		}
		date = date.Add(24 * time.Hour)
	}

	logger.Infof("total  profit = %.3f average day percent = %.3f", totalProfit, totalPercentage/float64(tradingDays))
}

func TestWithMultipleConfigs(b *bot.Bot, logger investgo.Logger, start, stop time.Time) {
	// слайс конфигов для бекстеста
	bc := make([]bot.BacktestConfig, 0)
	// начальные значения для стоп-лосса в процентах и кол-ва дней для расчета интервала=
	stopLoss := 0.4
	daysTocalculate := 1
	// простым перебором генерируем конфиги с разными значениями
	for stopLoss < 2 {
		for daysTocalculate < 7 {
			minProfit := 0.2
			for minProfit < 1.2 {
				bc = append(bc, bot.BacktestConfig{
					Analyse:                 bot.MinProfit,
					LowPercentile:           0,
					HighPercentile:          0,
					MinProfit:               minProfit,
					StopLoss:                stopLoss,
					DaysToCalculateInterval: daysTocalculate,
				})
				minProfit += 0.1
			}

			//tempPerc := 1.0
			//for tempPerc < 45 {
			//	bc = append(bc, bot.BacktestConfig{
			//		Analyse:                 bot.MathStat,
			//		LowPercentile:           math.Round(tempPerc),
			//		HighPercentile:          math.Round(100 - tempPerc),
			//		MinProfit:               0.2,
			//		StopLoss:                stopLoss,
			//		DaysToCalculateInterval: daysTocalculate,
			//	})
			//	tempPerc += 1
			//}
			daysTocalculate++
		}
		stopLoss += 0.1
	}
	// слайс отчетов по бектесту
	reports := make([]Report, 0)
	// проверяем на истории каждый сгенерированный конфиг
	for _, config := range bc {
		initDate := start
		stopDate := stop

		date := initDate
		var totalPercentage, totalProfit float64
		var tradingDays int

		for date.Before(stopDate) {
			profit, percentage, err := b.BackTest(date, config)
			if err != nil {
				logger.Errorf(err.Error())
			}
			logger.Infof("day profit = %.9f", profit)
			logger.Infof("day profit percent = %.9f", percentage)
			totalPercentage += percentage
			totalProfit += profit
			if totalProfit != 0 {
				tradingDays++
			}
			date = date.Add(24 * time.Hour)
		}
		// средний процент профита в день считаем как сумму всех профитов за день в процентах / кол-во торговых дней(когда профит != 0)
		var ap float64
		if tradingDays > 0 {
			ap = totalPercentage / float64(tradingDays)
		}
		reports = append(reports, Report{
			bc:                      config,
			totalProfit:             totalProfit,
			averageDayPercentProfit: ap,
		})
		tradingDays = 0
	}
	// сортируем отчеты по возрастанию профита
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].averageDayPercentProfit < reports[j].averageDayPercentProfit
	})

	for i, report := range reports {
		fmt.Printf("report %v:\naverage day profit percent =  %.3f\ntotal profit =  %.3f\nanalyse = %v\nminProfit = %v\nlowPercentile = %v\nhighPercentile = %v\n"+
			"days = %v\nstopLoss = %v\n\n",
			i, report.averageDayPercentProfit, report.totalProfit, report.bc.Analyse, report.bc.MinProfit, report.bc.LowPercentile,
			report.bc.HighPercentile, report.bc.DaysToCalculateInterval, report.bc.StopLoss)
	}
}
