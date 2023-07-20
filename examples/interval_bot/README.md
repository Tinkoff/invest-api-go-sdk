## Интервальный робот (Уровневый, Сеточный)

### Стратегия
Есть бумаги с высокой волатильностью, цены которых часто ходят в рамках коридора. Цель сеточного бота - найти подходящие 
бумаги и значения границ этого коридора, чтобы автоматически покупать по нижней границе и продавать по верхней границе.

#### Конфигурация
```go
type IntervalStrategyConfig struct {
// Instruments - слайс идентификаторов инструментов
Instruments []string
// PreferredPositionPrice - Предпочтительная стоимость открытия позиции в валюте
PreferredPositionPrice float64
// MaxPositionPrice - Максимальная стоимость открытия позиции в валюте
MaxPositionPrice float64
// MinProfit - Минимальный процент выгоды, с которым можно совершать сделки
MinProfit float64
// IntervalUpdateDelay - Время ожидания для перерасчета интервала цены
IntervalUpdateDelay time.Duration
// TopInstrumentsQuantity - Топ лучших инструментов по волатильности
TopInstrumentsQuantity int
// SellOut - Если true, то по достижению дедлайна бот выходит из всех активных позиций
SellOut bool
// StorageDBPath - Путь к бд sqlite, в которой лежат исторические свечи по инструментам
StorageDBPath string
// StorageCandleInterval - Интервал для обновления и запроса исторических свечей
StorageCandleInterval pb.CandleInterval
// StorageFromTime - Время, от которого будет хранилище будет загружать историю для новых инструментов
StorageFromTime time.Time
// StorageUpdate - Если true, то в хранилище обновятся все свечи до now
StorageUpdate bool
// DaysToCalculateInterval - Кол-во дней, на которых рассчитывается интервал цен для торговли
DaysToCalculateInterval int
// StopLossPercent - Процент изменения цены, для стоп-лосс заявки
StopLossPercent float64
// AnalyseLowPercentile - Нижний процентиль для расчета интервала
AnalyseLowPercentile float64
// AnalyseHighPercentile - Верхний процентиль для расчета интервала
AnalyseHighPercentile float64
}
```

### Исполнитель
Под стратегию написан исполнитель, он выставляет лимитные торговые поручения. Так же реализован механизм отслеживания 
последних цен для стоп-лосс заявки и стоп-лимит заявки на покупку

**Покупка**

Заявка на покупку *не* выставляется если:
* Позиция уже открыта
* На счету недостаточно денежных средств
* Цена последней сделки меньше нижней границы интервала

**Продажа**

Заявка на продажу *не* выставляется если:
* Позиция не открыта

### Режим работы
Данный пример ориентирован на торговлю внутри одного дня. За расписанием торгов следит `investgo.Timer`,
он сигнализирует о начале и завершении основной торговй сессии на сегодня.
При запуске main `investgo.Timer` возвращает канал с событиями, START/STOP - сигналы к запуску и остановке бота,
если выставлен флаг `SellOut` в конфигурации стратеги и время `cancelAhead` при создании таймера, то бот завершит работу и закроет все
позиции за `cancelAhead` до конца торгов текущего дня.

### Запуск
Клонируйте репозиторий

    $ git clone https://github.com/tinkoff/invest-api-go-sdk

Перейдите в папку с ботом

    $ cd invest-api-go-sdk/examples/interval_bot

Создайте файл `config.yaml`

    $ touch "config.yaml"

И заполните его по примеру `example.yaml`

```yaml
AccountId: ""
APIToken: <your_token>
EndPoint: sandbox-invest-public-api.tinkoff.ru:443
AppName: invest-api-go-sdk
DisableResourceExhaustedRetry: false
DisableAllRetry: false
MaxRetries: 3
```

*Для быстрого старта на песочнице достаточно указать только токен, остальное заполнится по умолчанию.*

    $ go run cmd/main.go

Обратите внимание, что в одной функции main есть возможность создать несколько клиентов для investAPI c разными
токенами и счетами, а с разными клиентами можно создавать разных ботов и запускать их одновременно. 