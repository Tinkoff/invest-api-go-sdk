package main

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	"go.uber.org/zap"
	"log"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	// Загружаем конфигурацию для сдк
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Println("Cnf loading error", err.Error())
	}
	// контекст будет передан в сдк и будет использоваться для завершения работы
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()
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

	// для синхронизации всех горутин
	wg := &sync.WaitGroup{}

	ordersStreamClient := client.NewOrdersStreamClient()

	tradesStream, err := ordersStreamClient.TradesStream([]string{config.AccountId})
	if err != nil {
		logger.Fatalf(err.Error())
	}

	// получаем канал для чтения
	trades := tradesStream.Trades()

	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case trade, ok := <-trades:
				if !ok {
					return
				}
				fmt.Printf("trade id = %v, direction %v\n", trade.GetOrderId(), trade.GetDirection().String())
			}
		}
	}(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := tradesStream.Listen()
		if err != nil {
			logger.Fatalf(err.Error())
		}
	}()

	wg.Wait()
}
