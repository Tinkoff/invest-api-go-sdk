package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type Order struct {
	Price    float64 `json:"Price"`
	Quantity int64   `json:"Quantity"`
}

type OrderBook struct {
	Figi          string  `json:"Figi"`
	InstrumentUid string  `json:"InstrumentUid"`
	Depth         int32   `json:"Depth"`
	IsConsistent  bool    `json:"IsConsistent"`
	TimeUnix      int64   `json:"TimeUnix"`
	LimitUp       float64 `json:"LimitUp"`
	LimitDown     float64 `json:"LimitDown"`
	Bids          []Order `json:"Bids"`
	Asks          []Order `json:"Asks"`
}

func main() {
	config, err := investgo.LoadConfig("config.yaml")
	if err != nil {
		log.Printf("Config loading error %v", err.Error())
	}

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
		log.Fatalf("logger creating error %v", err)
	}
	logger := prod.Sugar()

	client, err := investgo.NewClient(ctx, config, logger)
	if err != nil {
		logger.Errorf("Client creating error %v", err.Error())
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

	MarketDataStreamService := client.NewMarketDataStreamClient()

	// создаем стрим
	stream, err := MarketDataStreamService.MarketDataStream()
	if err != nil {
		logger.Errorf(err.Error())
	}

	instruments := []string{"BBG004730N88", "BBG00475KKY8", "BBG004RVFCY3"}
	orderBooks, err := stream.SubscribeOrderBook(instruments, 20)

	orderBookStorage := make(chan *OrderBook)
	defer close(orderBookStorage)

	orderBookTransform := make(chan *pb.OrderBook)
	defer close(orderBookTransform)

	// читаем стаканы из канала
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-orderBooks:
				if !ok {
					return
				}
				orderBookTransform <- ob
			}
		}
	}(ctx)

	// запускаем чтение стрима
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		err := stream.Listen()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}(ctx)

	// преобразуем стакан в нужную структуру
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case input, ok := <-orderBookTransform:
				if !ok {
					return
				}
				orderBookStorage <- transformOrderBook(input)
			}
		}
	}(ctx)

	jsonFile := make(chan []OrderBook)
	defer close(jsonFile)
	// сохраняем в хранилище
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		hundred := make([]OrderBook, 100)
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case ob, ok := <-orderBookStorage:
				if !ok {
					return
				}
				if count < 99 {
					hundred[count] = *ob
					count++
				} else {
					fmt.Println("hundred overflow")
					hundred[count] = *ob
					count = 0
					jsonFile <- hundred
				}
			}
		}
	}(ctx)

	// записываем 100 стаканов в json файл
	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-jsonFile:
				if !ok {
					return
				}
				err := writeOrderBooksToFile(data)
				if err != nil {
					logger.Infof(err.Error())
				}
			}
		}
	}(ctx)

	wg.Wait()
}

func transformOrderBook(input *pb.OrderBook) *OrderBook {
	depth := input.GetDepth()
	bids := make([]Order, 0, depth)
	asks := make([]Order, 0, depth)
	for _, o := range input.GetBids() {
		bids = append(bids, Order{
			Price:    o.GetPrice().ToFloat(),
			Quantity: o.GetQuantity(),
		})
	}
	for _, o := range input.GetAsks() {
		asks = append(asks, Order{
			Price:    o.GetPrice().ToFloat(),
			Quantity: o.GetQuantity(),
		})
	}
	return &OrderBook{
		Figi:          input.GetFigi(),
		InstrumentUid: input.GetInstrumentUid(),
		Depth:         depth,
		IsConsistent:  input.GetIsConsistent(),
		TimeUnix:      input.GetTime().AsTime().Unix(),
		LimitUp:       input.GetLimitUp().ToFloat(),
		LimitDown:     input.GetLimitDown().ToFloat(),
		Bids:          bids,
		Asks:          asks,
	}
}

func writeOrderBooksToFile(orderBooks []OrderBook) error {
	file, err := os.Create("examples/json/order_books.json")
	if err != nil {
		return err
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Printf(err.Error())
		}
	}()
	data, err := json.MarshalIndent(orderBooks, "", " ")
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}
