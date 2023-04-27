package main

import (
	"context"
	"fmt"
	"github.com/Tinkoff/invest-api-go-sdk/investgo"
	"go.uber.org/zap"
	"log"
	"os"
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
	ctx, cancel := context.WithCancel(context.Background())
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

	// для синхронизации всех горутин
	wg := &sync.WaitGroup{}

	OSClient := client.NewOperationsStreamClient()

	positionsStream, err := OSClient.PositionsStream([]string{config.AccountId})
	if err != nil {
		client.Logger.Errorf(err.Error())
	}

	portfolioStream, err := OSClient.PortfolioStream([]string{config.AccountId})
	if err != nil {
		client.Logger.Errorf(err.Error())
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
			client.Logger.Errorf(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := portfolioStream.Listen()
		if err != nil {
			client.Logger.Errorf(err.Error())
		}
	}()

	<-signals
	cancel()

	wg.Wait()
}
