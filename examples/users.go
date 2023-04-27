package main

import (
	"context"
	"fmt"
	"github.com/Tinkoff/invest-api-go-sdk/investgo"
	"go.uber.org/zap"
	"log"
	"os"
	"os/signal"
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
	// создаем клинета для сервиса счетов
	usersService := client.NewUsersServiceClient()

	var accId string
	accsResp, err := usersService.GetAccounts()
	if err != nil {
		logger.Error(err.Error())
	} else {
		accs := accsResp.GetAccounts()
		for _, acc := range accs {
			accId = acc.GetId()
			fmt.Printf("account id = %v\n", accId)
		}
	}

	marginResp, err := usersService.GetMarginAttributes(accId)
	if err != nil {
		logger.Error(err.Error())
	} else {
		fmt.Printf("liquid portfolio moneyvalue = %v\n", marginResp.GetLiquidPortfolio().ToFloat())
	}
}
