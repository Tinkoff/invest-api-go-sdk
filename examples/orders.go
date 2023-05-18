package main

import (
	"context"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
)

func main() {
	// Созадем клиента с grpc connection
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("config loading error %v", err.Error())
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
		logger.Fatalf("Client creating error %v", err.Error())
	}
	defer func() {
		logger.Infof("Closing client connection")
		err := client.Stop()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	OrdersService := client.NewOrdersServiceClient()

	buyResp, err := OrdersService.Buy(&investgo.PostOrderRequestShort{
		InstrumentId: "BBG004S681W1",
		Quantity:     1,
		Price:        nil,
		AccountId:    config.AccountId,
		OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
		OrderId:      investgo.CreateUid(),
	})

	if err != nil {
		logger.Errorf("buy order %v", err.Error())
	}
	// можем извлечь метаданные из ответа
	fmt.Printf("remaining ratelimit = %v\n", investgo.RemainingLimitFromHeader(buyResp.GetHeader()))
	sellResp, err := OrdersService.Sell(&investgo.PostOrderRequestShort{
		InstrumentId: "BBG004S681W1",
		Quantity:     1,
		Price:        nil,
		AccountId:    config.AccountId,
		OrderType:    pb.OrderType_ORDER_TYPE_MARKET,
		OrderId:      investgo.CreateUid(),
	})
	if err != nil {
		logger.Errorf("sell order %v\n", err.Error())
		// еще можно извлечь сообщение об ошибке из заголовка
		fmt.Printf("msg = %v\n", investgo.MessageFromHeader(sellResp.GetHeader()))
	} else {
		fmt.Printf("sell resp, status = %v\n", sellResp.GetExecutionReportStatus().String())
	}

	executedPrice := buyResp.GetExecutedOrderPrice()
	limitOrderId := investgo.CreateUid()

	postResp, err := OrdersService.PostOrder(&investgo.PostOrderRequest{
		InstrumentId: "BBG004S681W1",
		Quantity:     1,
		Price: &pb.Quotation{
			Units: executedPrice.Units + 100,
			Nano:  executedPrice.Nano,
		},
		Direction: pb.OrderDirection_ORDER_DIRECTION_BUY,
		AccountId: config.AccountId,
		OrderType: pb.OrderType_ORDER_TYPE_LIMIT,
		OrderId:   limitOrderId,
	})
	if err != nil {
		logger.Errorf("post order %v\n", err.Error())
	} else {
		fmt.Printf("post order resp = %v\n", postResp.GetExecutionReportStatus().String())
	}

	orderResp, err := OrdersService.GetOrderState(config.AccountId, limitOrderId)
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		fmt.Printf("/from get order state/ order id = %v, direction =  %v, lots executed = %v\n", orderResp.GetOrderId(), orderResp.GetDirection(), orderResp.GetLotsExecuted())
	}

	ordersResp, err := OrdersService.GetOrders(config.AccountId)
	if err != nil {
		logger.Errorf(err.Error())
	} else {
		orders := ordersResp.GetOrders()
		for _, order := range orders {
			fmt.Printf("order id = %v, direction =  %v, lots executed = %v\n", order.GetOrderId(), order.GetDirection(), order.GetLotsExecuted())
		}
	}

}
