package main

import (
	"context"
	"fmt"
	"github.com/sourcegraph/conc/pool"
	"github.com/tinkoff/invest-api-go-sdk/examples/interval_bot/internal/bot"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"math"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
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

// RunMode - Режим запуска бектеста
type RunMode int

const (
	// TEST_WITH_CONFIG - Запуск бектеста на одном конфиге configToTest
	TEST_WITH_CONFIG RunMode = iota
	// TEST_WITH_MULTIPLE_CONFIGS - Запуск генерации конфигов полным перебором и проверка их всех
	TEST_WITH_MULTIPLE_CONFIGS
)

const (
	// INSTRUMENTS_MAX - Максимальное кол-во инструментов
	INSTRUMENTS_MAX = 300
	// EXCHANGE - Биржа на которой будет работать бот
	EXCHANGE = "MOEX"
	// CURRENCY - Валюта для работы бота
	CURRENCY = "RUB"
)

var (
	// Интервал для проверки
	initDate = time.Date(2023, 6, 22, 0, 0, 0, 0, time.Local)
	stopDate = time.Date(2023, 7, 7, 0, 0, 0, 0, time.Local)
	// Критерий для отбора бумаг. Акции, фонды, акции и фонды
	selection = SHARES_AND_ETFS
	// Режим запуска теста, на одном конфиге или перебор сгенерированных конфигов
	mode = TEST_WITH_MULTIPLE_CONFIGS
	// Конфигурация стратегии, остальные поля заполняются из конфига бектеста
	intervalConfig = bot.IntervalStrategyConfig{
		PreferredPositionPrice: 1000,
		MaxPositionPrice:       5000,
		TopInstrumentsQuantity: 10,
		SellOut:                true,
		StorageDBPath:          "examples/interval_bot/candles/candles.db",
		StorageCandleInterval:  pb.CandleInterval_CANDLE_INTERVAL_1_MIN,
		StorageFromTime:        time.Date(2023, 1, 10, 0, 0, 0, 0, time.Local),
		StorageUpdate:          false,
	}
	// Конфиг бектеста для режима TEST_WITH_CONFIG
	configToTest = bot.BacktestConfig{
		Analyse:                 bot.MinProfit,
		LowPercentile:           0,
		HighPercentile:          0,
		MinProfit:               0.3,
		DaysToCalculateInterval: 2,
		StopLoss:                2,
		// Для тарифа "Трейдер" комиссия за сделку с акцией составляет 0.05% от стоимости сделки
		Commission: 0.05,
	}
	// Границы параметров конфига бектеста для генерации
	stopLossMin = 0.5
	stopLossMax = 2.0

	daysToCalculateMin = 1
	daysToCalculateMax = 5

	minProfitMin = 0.2
	minProfitMax = 0.6

	percentileMin = 3.0
	percentileMax = 40.0
)

// Report - Отчет о тесте на конкретном конфиге
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
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigs
		cancel()
	}()
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
	// получаем список акций доступных для торговли через investAPI
	sharesResp, err := insrtumentsService.Shares(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Errorf(err.Error())
	}
	// рублевые акции c московской биржи
	shares := sharesResp.GetInstruments()

	switch selection {
	case SHARES:
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
	case ETFS:
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
	case SHARES_AND_ETFS:
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
	}

	logger.Infof("got %v instruments", len(instrumentIds))
	// Добавляем инструменты в конфиг
	intervalConfig.Instruments = instrumentIds
	// создание интервального бота
	intervalBot, err := bot.NewBot(ctx, client, intervalConfig)
	if err != nil {
		logger.Fatalf("interval bot creating fail %v", err.Error())
	}
	// выбираем режим запуска
	switch mode {
	case TEST_WITH_CONFIG:
		TestWithConfig(ctx, intervalBot, logger, initDate, stopDate, configToTest)
	case TEST_WITH_MULTIPLE_CONFIGS:
		TestWithMultipleConfigs(ctx, intervalBot, logger, initDate, stopDate)
	}
}

// TestWithConfig - Проверка на одном конфиге
func TestWithConfig(ctx context.Context, b *bot.Bot, logger investgo.Logger, start, stop time.Time, config bot.BacktestConfig) {
	r, err := testConfig(ctx, b, start, stop, config)
	if err != nil {
		logger.Errorf(err.Error())
	}
	logger.Infof("total  profit = %.3f average day percent = %.3f", r.totalProfit, r.averageDayPercentProfit)
}

// TestWithMultipleConfigs - Генерация мнодетсва конфигов и проверка на них
func TestWithMultipleConfigs(ctx context.Context, b *bot.Bot, logger investgo.Logger, start, stop time.Time) {
	// слайс конфигов для бекстеста
	bc := make([]bot.BacktestConfig, 0)
	// начальные значения для стоп-лосса в процентах и кол-ва дней для расчета интервала=
	stopLoss := stopLossMin
	// простым перебором генерируем конфиги с разными значениями
	for stopLoss < stopLossMax {
		daysToCalculate := daysToCalculateMin
		for daysToCalculate < daysToCalculateMax {
			minProfit := minProfitMin
			for minProfit < minProfitMax {
				bc = append(bc, bot.BacktestConfig{
					Analyse:                 bot.MinProfit,
					LowPercentile:           0,
					HighPercentile:          0,
					MinProfit:               minProfit,
					StopLoss:                stopLoss,
					DaysToCalculateInterval: daysToCalculate,
					Commission:              0.05,
				})
				minProfit += 0.1
			}

			tempPerc := percentileMin
			for tempPerc < percentileMax {
				bc = append(bc, bot.BacktestConfig{
					Analyse:                 bot.MathStat,
					LowPercentile:           math.Round(tempPerc),
					HighPercentile:          math.Round(100 - tempPerc),
					MinProfit:               minProfit,
					StopLoss:                stopLoss,
					DaysToCalculateInterval: daysToCalculate,
				})
				tempPerc += 1
			}
			daysToCalculate++
		}
		stopLoss += 0.1
	}

	// Запускаем параллельно проверку всех конфигов, которые получили выше
	rp := pool.NewWithResults[Report]().WithMaxGoroutines(runtime.NumCPU()).WithContext(ctx)
	for _, config := range bc {
		c := config
		rp.Go(func(ctx context.Context) (Report, error) {
			return testConfig(ctx, b, start, stop, c)
		})
	}
	// по каждому конфигу будет отчет с профитом
	reports, err := rp.Wait()
	if err != nil {
		logger.Errorf(err.Error())
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

// testConfig - Бектест для конфига на времени start-stop
func testConfig(ctx context.Context, b *bot.Bot, start, stop time.Time, config bot.BacktestConfig) (Report, error) {
	initDate := start
	stopDate := stop

	date := initDate
	var totalPercentage, totalProfit float64
	var tradingDays int

	var done bool
	for date.Before(stopDate) && !done {
		select {
		case <-ctx.Done():
			done = true
		default:
			profit, percentage, err := b.BackTest(date, config)
			if err != nil {
				return Report{}, err
			}
			totalPercentage += percentage
			totalProfit += profit
			if totalProfit != 0 {
				tradingDays++
			}
			date = date.Add(24 * time.Hour)
		}
	}
	// средний процент профита в день считаем как сумму всех профитов за день в процентах / кол-во торговых дней(когда профит != 0)
	var ap float64
	if tradingDays > 0 {
		ap = totalPercentage / float64(tradingDays)
	}
	return Report{
		bc:                      config,
		totalProfit:             totalProfit,
		averageDayPercentProfit: ap,
	}, nil
}
