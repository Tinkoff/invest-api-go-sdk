# INVEST API Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/tinkoff/invest-api-go-sdk.svg)](https://pkg.go.dev/github.com/tinkoff/invest-api-go-sdk)

SDK предназначен для упрощения работы с API Тинькофф Инвестиций.

## Начало работы

    $ go get github.com/tinkoff/invest-api-go-sdk

### Где взять токен аутентификации?

В разделе инвестиций вашего  [личного кабинета tinkoff](https://www.tinkoff.ru/invest/) . Далее:

* Перейдите в [настройки](https://www.tinkoff.ru/invest/settings/)
* Проверьте, что функция “Подтверждение сделок кодом” отключена
* Выпустите токен (если не хотите через API выдавать торговые поручения, то надо выпустить токен "только для чтения")  
* Скопируйте токен и сохраните, токен отображается только один раз, просмотреть его позже не получится, тем не менее вы можете выпускать неограниченное количество токенов. Токен передается в библиотеку в методе инициализации SDKInit() 

## Документация

Документацию непосредственно по INVEST API можно найти по [ссылке](https://github.com/Tinkoff/investAPI).

### Быстрый старт

Для непосредственного взаимодействия с INVEST API нужно создать клиента. 
Примеры использования SDK находятся в директории examples:
 * `md_stream.go`, orders_stream.go, operations_stream.go - примеры работы со стримами
 * `instruments.go` - примеры работы с сервисом инструментов
 * `marketdata.go` - примеры работы с сервисом котировок
 * `operations.go` - примеры работы с сервисом операций
 * `orders.go` - примеры работы с сервисом торговых поручений
 * `stop_orders` - примеры работы с сервисом стоп-заявок
 * `users.go` - примеры работы с сервисом счетов
 * `sandbox.go` - пример работы с песочницей
 * `order_book_download/order_book.go` - пример сохранения стаканов из стрима маркетдаты в sqlite или json

#### Конфигурация SDK
```go
type Config struct {
// EndPoint - Для работы с реальным контуром и контуром песочницы нужны разные эндпоинты.
// По умолчанию = sandbox-invest-public-api.tinkoff.ru:443
// https://tinkoff.github.io/investAPI/url_difference/
EndPoint string `yaml:"EndPoint"`
// Token - Ваш токен для Tinkoff InvestAPI
Token string `yaml:"APIToken"`
// AppName - Название вашего приложения, по умолчанию = tinkoff-api-go-sdk
AppName string `yaml:"AppName"`
// AccountId - Если уже есть аккаунт для апи можно указать напрямую,
// для песочницы создастся и запишется автоматически
AccountId string `yaml:"AccountId"`
// DisableResourceExhaustedRetry - Если true, то сдк не пытается ретраить, после получения ошибки об исчерпывании
// лимита запросов, если false, то сдк ждет нужное время и пытается выполнить запрос снова. По умолчанию = false
DisableResourceExhaustedRetry bool `yaml:"DisableResourceExhaustedRetry"`
// DisableAllRetry - Отключение всех ретраев
DisableAllRetry bool `yaml:"DisableAllRetry"`
// MaxRetries - Максимальное количество попыток переподключения, по умолчанию = 3
// (если указать значение 0 это не отключит ретраи, для отключения нужно прописать DisableAllRetry = true)
MaxRetries uint `yaml:"MaxRetries"`
}
```
Для проверки достаточно указать токен и запустить пример `sandbox.go`

### Дополнительные возможности
* **Загрузка исторических данных.** В рамках сервиса `Marketdata`, метод `GetHistoricCandles` возвращает список
свечей в интервале (from - to), метод `GetAllHistoricCandles` возвращает все доступные свечи.
* **Получение метеданных.** В теле ответа Unary - методов присутствует `grpc.Header`, при момощи методов 
`investgo.MessageFromHeader` и `investgo.RemainingLimitFromHeader` вы можете получить сообщение ошибки, 
и текущий остаток запросов соответсвенно. Подробнее про заголовки [тут](https://tinkoff.github.io/investAPI/grpc/)
* **Переподключение.** По умолчанию включен ретраер, который при получении ошибок от grpc пытается выполнить запрос повторно,
а в случае со стримами переподклчается и переподписывает стрим на всю подписки. Отдельно можно 
отключить ретраер для ошибки `ResourceExhausted`, по умолчанию он включен и в случае превышения лимитов Unary - запросов,
ретраер ждет нужное время и продолжает выполнение, *при этом никакого сообщения об ошибке для клиента нет*.

#### Пример использования MarketDataStreamService
```go
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
	// инициализация... //
    
	// создаем клиента для апи инвестиций, он поддерживает grpc соединение
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

	// один раз создаем клиента для стримов
	MDClient := client.NewMarketDataStreamClient()

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
    
	// полный пример - examples/md_stream.go // 
}

```
### У меня есть вопрос

[Основной репозиторий с документацией](https://github.com/Tinkoff/investAPI/) — в нем вы можете задать вопрос в Issues и получать информацию о релизах в Releases.
Если возникают вопросы по данному SDK, нашёлся баг или есть предложения по улучшению, то можно задать его в Issues этого репозитория.
