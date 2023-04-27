package main

import (
	"context"
	"fmt"
	"github.com/Tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Загружаем конфигурацию для сдк
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Println("Cnf loading error", err.Error())
	}
	// контекст будет передан в сдк и будет использоваться для завершения работы
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal)
	defer close(signals)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// Для примера передадим к качестве логгера uber zap
	prod, err := zap.NewProduction()
	defer func() {
		err := prod.Sync()
		if err != nil {
			log.Printf("Prod.Sync %v", err.Error())
		}
	}()

	if err != nil {
		log.Fatalf("logger creating error %e", err)
	}
	logger := prod.Sugar()

	// Создаем клиеинта для апи инвестиций, он поддерживает grpc соединение
	client, err := investgo.NewClient(ctx, config, logger)
	if err != nil {
		logger.Infof("Client creating error %v", err.Error())
	}
	defer func() {
		logger.Infof("Closing client connection")
		err := client.Stop()
		if err != nil {
			logger.Error("client shutdown error %v", err.Error())
		}
	}()
	// создаем клинета для сервиса маркетдаты
	MarketDataService := client.NewMarketDataServiceClient()
	// три российские акции
	instruments := []string{"BBG004730N88", "BBG00475KKY8", "BBG004RVFCY3"}

	from := time.Date(2023, time.April, 18, 0, 0, 0, 0, time.UTC)
	to := time.Date(2023, time.April, 19, 0, 0, 0, 0, time.UTC)

	candlesResp, err := MarketDataService.GetCandles(instruments[0], pb.CandleInterval_CANDLE_INTERVAL_HOUR, from, to)
	if err != nil {
		logger.Error(err.Error())
	} else {
		candles := candlesResp.GetCandles()
		for i, candle := range candles {
			fmt.Printf("candle number %d, low price = %v\n", i, candle.GetLow().ToFloat())
		}
	}

	tradingStatusResp, err := MarketDataService.GetTradingStatus(instruments[1])
	if err != nil {
		logger.Error(err.Error())
	} else {
		fmt.Printf("trading status = %v\n", tradingStatusResp.GetTradingStatus())
	}

	lastPriceResp, err := MarketDataService.GetLastPrices(instruments)
	if err != nil {
		logger.Error(err.Error())
	} else {
		lp := lastPriceResp.GetLastPrices()
		for i, price := range lp {
			fmt.Printf("last price number %v = %v\n", i, price.GetPrice().ToFloat())
		}
	}

	orderBookResp, err := MarketDataService.GetOrderBook(instruments[2], 1)
	if err != nil {
		logger.Error(err.Error())
	} else {
		fmt.Printf("order book bids = %v\n", orderBookResp.GetBids())
	}

	lastTradesResp, err := MarketDataService.GetLastTrades(instruments[1], time.Now().Add(-1*time.Minute), time.Now())
	if err != nil {
		logger.Error(err.Error())
	} else {
		lt := lastTradesResp.GetTrades()
		for i, trade := range lt {
			fmt.Printf("last trade number %v, price = %v\n", i, trade.GetPrice().ToFloat())
		}
	}

	closePriceResp, err := MarketDataService.GetClosePrices(instruments)
	if err != nil {
		logger.Error(err.Error())
	} else {
		cp := closePriceResp.GetClosePrices()
		for _, price := range cp {
			fmt.Printf("close price = %v\n", price.GetPrice().ToFloat())
		}
	}

}
