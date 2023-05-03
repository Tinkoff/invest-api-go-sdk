package main

import (
	"context"
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

	// создаем клиента для апи инвестиций, он поддерживает grpc соединение
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

	// один раз создаем клиента для стримов
	MDClient := client.NewMDStreamClient()

	// создаем стримов сколько нужно, например 2
	firstMDStream, err := MDClient.MarketDataStream()
	if err != nil {
		logger.Errorf(err.Error())
	}
	// результат подписки на инструменты это канал с определенным типом информации, при повторном вызове функции
	// подписки(например на свечи), возвращаемый канал можно игнорировать, так как при первом вызове он уже был получен
	firstInstrumetsGroup := []string{"BBG004730N88", "BBG00475KKY8", "BBG004RVFCY3"}
	candleChan, err := firstMDStream.SubscribeCandle(firstInstrumetsGroup, pb.SubscriptionInterval_SUBSCRIPTION_INTERVAL_ONE_MINUTE)
	if err != nil {
		logger.Errorf(err.Error())
	}

	tradesChan, err := firstMDStream.SubscribeTrade(firstInstrumetsGroup)
	if err != nil {
		logger.Errorf(err.Error())
	}

	// функцию Listen нужно вызвать один раз для каждого стрима и в отдельной горутине
	// для останвки стрима можно использовать метод Stop, он отменяет контекст внутри стрима
	// после вызова Stop закрываются каналы и завершается функция Listen
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := firstMDStream.Listen()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	// для дальнейшей обработки, поступившей из канала, информации хорошо подойдет механизм,
	// основанный на паттерне pipeline https://go.dev/blog/pipelines

	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				logger.Infof("Stop listening first channels")
				return
			case candle, ok := <-candleChan:
				if !ok {
					return
				}
				// клиентская логика обработки...
				fmt.Println("high price = ", candle.GetHigh().ToFloat())
			case trade, ok := <-tradesChan:
				if !ok {
					return
				}
				// клиентская логика обработки...
				fmt.Println("trade price = ", trade.GetPrice().ToFloat())
			}
		}
	}(ctx)

	// Для еще одного стрима в этом grpc.conn //
	secondMDStream, err := MDClient.MarketDataStream()
	if err != nil {
		logger.Errorf(err.Error())
	}

	secondInstrumetsGroup := []string{"BBG004S681W1", "BBG004731354"}
	obChan, err := secondMDStream.SubscribeOrderBook(secondInstrumetsGroup, 10)
	if err != nil {
		logger.Errorf(err.Error())
	}

	lastPriceChan, err := secondMDStream.SubscribeLastPrice(secondInstrumetsGroup)
	if err != nil {
		logger.Errorf(err.Error())
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := secondMDStream.Listen()
		if err != nil {
			logger.Errorf(err.Error())
		}
	}()

	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				logger.Infof("Stop listening second channels")
				return
			case ob, ok := <-obChan:
				if !ok {
					return
				}
				fmt.Println("order book time is = ", ob.GetTime().AsTime().String())
			case lp, ok := <-lastPriceChan:
				if !ok {
					return
				}
				fmt.Println("last price  = ", lp.GetPrice().ToFloat())
			}
		}
	}(ctx)

	<-signals
	cancel()

	wg.Wait()
}
