package main

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
	"time"
)

func main() {
	// создаем клиента с grpc connection
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Println("Cnf loading error", err.Error())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prod, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger creating error %e", err)
	}
	logger := prod.Sugar()

	client, err := investgo.NewClient(ctx, config, logger)
	if err != nil {
		log.Printf("Client creating error %e", err)
	}
	defer func() {
		logger.Infof("Closing client connection")
		err := client.Stop()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	operationsService := client.NewOperationsServiceClient()

	portfolioResp, err := operationsService.GetPortfolio(config.AccountId, pb.PortfolioRequest_RUB)
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		fmt.Printf("amount of shares = %v\n", portfolioResp.GetTotalAmountShares())
	}

	operationsResp, err := operationsService.GetOperations(&investgo.GetOperationsRequest{
		Figi:      "BBG004S681W1",
		AccountId: config.AccountId,
		State:     pb.OperationState_OPERATION_STATE_EXECUTED,
		From:      time.Now().Add(-10 * time.Hour),
		To:        time.Now(),
	})
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		ops := operationsResp.GetOperations()
		for i, op := range ops {
			fmt.Printf("operation %v, type = %v\n", i, op.GetOperationType().String())
		}
	}
	// для метода ниже песочница вернет []
	generateReportResp, err := operationsService.GenerateBrokerReport(config.AccountId, time.Now().Add(-1000*time.Hour), time.Now())
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		report := generateReportResp.String()
		fmt.Println(report)
	}

}
