package main

import (
	"fmt"
	"log"
	"time"

	investapi "github.com/tinkoff/invest-api-go-sdk"
)

//пример работает для "боевого" токена (можно read-only) для клиентов с минимум одним открытым брокерским счетов
const (
	token = "t.gXvyoB..." //вставьте Ваш токен
)

func main() {

	//Инициализируем SDK, первый параметр - токен доступа, второй параметр - использовать ли "песочницу" (true) или работать в "боевом" режиме (false)
	//Если инициализация не получится - выпадем в fatal

	SDKInit(token, false)

	//Получаем список акций, доступных для торгов через API
	shares, err := GetSharesBase()
	if err != nil {
		log.Fatalf("Невозможно получить список акций, ошибка - %s", err)
	}

	start, _ := time.Parse("2006-01-02", "2022-01-01")
	to := time.Now()

	// печатаем последнии цены по всем бумагам:
	lp, err := GetLastPricesForAll()
	if err != nil {
		log.Fatalf("Невозможно получить последнюю цену по всем бумагам, ошибка - %s", err)
	}
	fmt.Printf("%v\n", lp)

	//печатаем котировки для первых трех акций
	PrintCnt := 3
	for i := range shares {
		fmt.Printf("%v\n", shares[i])

		if i < PrintCnt {
			candles, err := GetCandles(shares[i].Figi, start, to, investapi.CandleInterval_CANDLE_INTERVAL_DAY)
			if err != nil {
				log.Fatalf("Невозможно получить свечи по акции %s, ошибка - %s", shares[i].Figi, err)
			}
			fmt.Printf("%v\n", candles)
		}
	}

	//Получаем список фондов, доступных для торгов через API
	etfs, err := GetETFsBase()
	if err != nil {
		log.Fatalf("Невозможно получить список фондов, ошибка - %s", err)
	}

	for i := range etfs {
		fmt.Printf("%v\n", etfs[i])
	}

	//Получаем список облигаций, доступных для торгов через API
	bonds, err := GetBondsBase()
	if err != nil {
		log.Fatalf("Невозможно получить список облигаций, ошибка - %s", err)
	}

	for i := range bonds {
		fmt.Printf("%v\n", bonds[i])
	}

	//Получаем список фьючерсов, доступных для торгов через API
	futures, err := GetFuturesBase()
	if err != nil {
		log.Fatalf("Невозможно получить список фьючерсов, ошибка - %s", err)
	}

	for i := range futures {
		fmt.Printf("%v\n", futures[i])
	}

	//Получаем список счетов
	accounts, err := GetAccounts()
	if err != nil {
		log.Fatalf("Невозможно получить список счетов, ошибка - %s", err)
	}
	account := ""
	for i := range accounts {
		account = accounts[i].Id
		fmt.Printf("%v\n", accounts[i])
	}

	//Получаем список операций с 01.01.2022
	log.Printf("Получаем список операций по сч %s... \n", account)

	start, _ = time.Parse("2006-01-02", "2022-01-01")
	to = time.Now()
	operations, err := GetOperations(account, start, to, "")
	if err != nil {
		log.Fatalf("Невозможно получить список операций, ошибка - %s, %s", err, account)
	}

	for i := range operations {
		fmt.Printf("%v\n", operations[i])
	}

	//Получаем портфолио
	log.Printf("Получаем портфель по сч %s... \n", account)
	portfolio, err := GetPortfolio(account)
	if err != nil {
		log.Fatalf("Невозможно получить портфолио, ошибка - %s", err)
	}
	for i := range portfolio {
		fmt.Printf("%v\n", portfolio[i])
	}

	//Получаем позиции
	log.Printf("Получаем позиции по сч %s... \n", account)
	positions, err := GetPositions(account)
	if err != nil {
		log.Fatalf("Невозможно получить список позиций, ошибка - %s", err)
	}
	for i := range positions.Securities {
		fmt.Printf("Ценные бумаги: %v\n", positions.Securities[i])
	}
	for i := range positions.Money {
		fmt.Printf("Валюта: %v\n", positions.Money[i])
	}
	for i := range positions.Futures {
		fmt.Printf("Фьючерсы: %v\n", positions.Futures[i])
	}
	for i := range positions.Blocked {
		fmt.Printf("Заблокировано: %v\n", positions.Blocked[i])
	}

	//Получаем детали по блокировкам средств
	withdrawLimit, err := WithdrawLimits(account)
	if err != nil {
		log.Fatalf("Невозможно получить список аккаунтов, ошибка - %s", err)
	}
	for i := range withdrawLimit.Blocked {
		fmt.Printf("Заблокировано: %v\n", withdrawLimit.Blocked[i])
	}
	for i := range withdrawLimit.BlockedGuarantee {
		fmt.Printf("Гарантийное обеспечение фьючерсов: %v\n", withdrawLimit.BlockedGuarantee[i])
	}

}
