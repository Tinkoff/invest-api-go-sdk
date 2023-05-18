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
		log.Fatalf("config loading error %v", err.Error())
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
		logger.Fatalf("Client creating error %v", err.Error())
	}
	defer func() {
		logger.Infof("Closing client connection")
		err := client.Stop()
		if err != nil {
			logger.Errorf("client shutdown error %v", err.Error())
		}
	}()

	// для синхронизации всех горутин
	wg := &sync.WaitGroup{}

	operationsStreamClient := client.NewOperationsStreamClient()

	positionsStream, err := operationsStreamClient.PositionsStream([]string{config.AccountId})
	if err != nil {
		logger.Errorf(err.Error())
	}

	portfolioStream, err := operationsStreamClient.PortfolioStream([]string{config.AccountId})
	if err != nil {
		logger.Errorf(err.Error())
	}
	// получаем каналы для чтения
	positions := positionsStream.Positions()
	portfolios := portfolioStream.Portfolios()

	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case pos, ok := <-positions:
				if !ok {
					return
				}
				fmt.Printf("Position %v", pos.String())
			case port, ok := <-portfolios:
				if !ok {
					return
				}
				fmt.Printf("Portfolio %v", port.String())
			}
		}
	}(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := positionsStream.Listen()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := portfolioStream.Listen()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	wg.Wait()
}
