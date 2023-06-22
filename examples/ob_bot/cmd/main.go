package main

import (
	"context"
	"errors"
	"github.com/tinkoff/invest-api-go-sdk/examples/ob_bot/internal/bot"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	// SHARES_NUM - Количество акций для торгов
	SHARES_NUM = 50
	// EXCHANGE - Биржа на которой будет работать бот
	EXCHANGE = "MOEX"
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
	prod, _ := zap.NewProduction()
	defer func() {
		err := prod.Sync()
		if err != nil {
			log.Printf("Prod.Sync %v", err.Error())
		}
	}()
	if err != nil {
		log.Fatalf("logger creating error %v", err)
	}
	logger := prod.Sugar()
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
		if len(instrumentIds) > SHARES_NUM-1 {
			break
		}
		if strings.Compare(share.GetExchange(), EXCHANGE) == 0 {
			instrumentIds = append(instrumentIds, share.GetUid())
		}
	}
	logger.Infof("got %v instruments\n", len(instrumentIds))

	instruments := instrumentIds
	// instruments := []string{"6afa6f80-03a7-4d83-9cf0-c19d7d021f76", "e6123145-9665-43e0-8413-cd61b8aa9b13"}

	// конфиг стратегии бота на стакане
	orderBookConfig := bot.OrderBookStrategyConfig{
		Instruments:  instruments,
		Depth:        20,
		BuyRatio:     2,
		SellRatio:    2,
		MinProfit:    0.5,
		SellOut:      true,
		SellOutAhead: 10 * time.Minute,
	}

	// дедлайн для интрадей торговли
	dd, err := tradingDeadLine(client, EXCHANGE)
	if err != nil {
		logger.Fatalf(err.Error())
	}

	// создание и запуск бота
	botOnOrderBook, err := bot.NewBot(ctx, client, dd, orderBookConfig)
	if err != nil {
		logger.Fatalf("bot on order book creating fail %v", err.Error())
	}

	go func() {
		<-sigs
		botOnOrderBook.Stop()
	}()

	err = botOnOrderBook.Run()
	if err != nil {
		logger.Errorf(err.Error())
	}
}

// tradingDeadLine - возвращает дедлайн торгов на сегодня, или ошибку если торговля на бирже сейчас недоступна
func tradingDeadLine(client *investgo.Client, exchange string) (time.Time, error) {
	from := time.Now()
	// так как основная сессия на бирже не больше 9 часов
	to := from.Add(time.Hour * 9)

	instrumentsService := client.NewInstrumentsServiceClient()
	resp, err := instrumentsService.TradingSchedules(exchange, from, to)
	if err != nil {
		return time.Time{}, err
	}

	var deadLine time.Time
	exchanges := resp.GetExchanges()
	for _, exch := range exchanges {
		// если нужная биржа
		if strings.Compare(exch.GetExchange(), exchange) == 0 {
			for _, day := range exch.GetDays() {
				// если день совпадает с днем запроса
				if from.Day() == day.GetDate().AsTime().Day() {
					switch {
					// выходной
					case !day.GetIsTradingDay():
						return time.Time{}, errors.New("trading isn't available today")
					// основная сессия либо еще не началась, либо уже закончилась
					case from.Before(day.GetStartTime().AsTime().Local()) || from.After(day.GetEndTime().AsTime().Local()):
						return time.Time{}, errors.New("from don't belong trading time")
					// положительный случай, возвращаем остаток до конца основной сессии
					case from.After(day.GetStartTime().AsTime().Local()) && from.Before(day.GetEndTime().AsTime().Local()):
						deadLine = day.GetEndTime().AsTime().Local()
					}
				}
			}
		}
	}
	return deadLine, nil
}
