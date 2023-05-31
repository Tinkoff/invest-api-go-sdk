package main

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	"go.uber.org/zap"
	"log"
	"os/signal"
	"syscall"
)

func main() {
	// загружаем конфигурацию для сдк из .yaml файла
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("config loading error %v", err.Error())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()
	// сдк использует для внутреннего логирования investgo.Logger
	// для примера передадим uber.zap
	prod := zap.NewExample()
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
	client, err := investgo.NewClient(ctx, config, logger)
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

	// создаем клиента для сервиса счетов
	usersService := client.NewUsersServiceClient()

	var accId string
	accsResp, err := usersService.GetAccounts()
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		accs := accsResp.GetAccounts()
		for _, acc := range accs {
			accId = acc.GetId()
			fmt.Printf("account id = %v\n", accId)
		}
	}

	marginResp, err := usersService.GetMarginAttributes(accId)
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		fmt.Printf("liquid portfolio moneyvalue = %v\n", marginResp.GetLiquidPortfolio().ToFloat())
	}
}
