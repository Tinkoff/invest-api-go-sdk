package main

import (
	"context"
	"fmt"
	"github.com/Tinkoff/invest-api-go-sdk/investgo"
	pb "github.com/Tinkoff/invest-api-go-sdk/proto"
	"go.uber.org/zap"
	"log"
	"time"
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

	// создаем клиента для сервиса инструментов
	instrumentsService := client.NewInstrumentsServiceClient()

	instrResp, err := instrumentsService.FindInstrument("TCSG")
	if err != nil {
		logger.Error(err.Error())
	} else {
		ins := instrResp.GetInstruments()
		for _, instrument := range ins {
			fmt.Printf("по запросу TCSG - %v\n", instrument.GetName())
		}
	}

	instrResp1, err := instrumentsService.FindInstrument("Тинькофф")
	if err != nil {
		logger.Error(err.Error())
	} else {
		ins := instrResp1.GetInstruments()
		for _, instrument := range ins {
			fmt.Printf("по запросу Тинькофф - %v, uid - %v\n", instrument.GetName(), instrument.GetUid())
		}
	}

	scheduleResp, err := instrumentsService.TradingSchedules("MOEX", time.Now(), time.Now().Add(time.Hour*24))
	if err != nil {
		logger.Error(err.Error())
	} else {
		exs := scheduleResp.GetExchanges()
		for _, ex := range exs {
			fmt.Printf("days = %v\n", ex.GetDays())
		}
	}

	// методы получения инструмента определенного типа по идентефикатору или методы получения списка
	// инструментов по торговому статусу аналогичны друг другу по входным параметрам
	tcsResp, err := instrumentsService.ShareByUid("6afa6f80-03a7-4d83-9cf0-c19d7d021f76")
	if err != nil {
		logger.Error(err.Error())
	} else {
		fmt.Printf("TCSG share currency -  %v, ipo date - %v\n",
			tcsResp.GetInstrument().GetCurrency(), tcsResp.GetInstrument().GetIpoDate().AsTime().String())
	}

	bondsResp, err := instrumentsService.Bonds(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Error(err.Error())
	} else {
		bonds := bondsResp.GetInstruments()
		for i, b := range bonds {
			fmt.Printf("bond %v = %v\n", i, b.GetFigi())
			if i > 4 {
				break
			}
		}
	}

	bond, err := instrumentsService.BondByFigi("BBG00QXGFHS6")
	if err != nil {
		logger.Error(err.Error())
	} else {
		fmt.Printf("bond by figi = %v\n", bond.GetInstrument().String())
	}

	interestsResp, err := instrumentsService.GetAccruedInterests("BBG00QXGFHS6", time.Now().Add(-72*time.Hour), time.Now())
	if err != nil {
		logger.Error(err.Error())
	} else {
		in := interestsResp.GetAccruedInterests()
		for _, interest := range in {
			fmt.Printf("Interest = %v\n", interest.GetValue().ToFloat())
		}
	}

	bondCouponsResp, err := instrumentsService.GetBondCoupons("BBG00QXGFHS6", time.Now(), time.Now().Add(time.Hour*10000))
	if err != nil {
		logger.Error(err.Error())
	} else {
		ev := bondCouponsResp.GetEvents()
		for _, coupon := range ev {
			fmt.Printf("coupon date = %v\n", coupon.GetCouponDate().AsTime().String())
		}
	}

	optionsResp, err := instrumentsService.Options(pb.InstrumentStatus_INSTRUMENT_STATUS_BASE)
	if err != nil {
		logger.Error(err.Error())
	} else {
		options := optionsResp.GetInstruments()
		for i, op := range options {
			fmt.Printf("option %v, ticker = %v, classcode = %v\n", i, op.GetTicker(), op.GetClassCode())
			if i > 4 {
				break
			}
		}
	}

	optionByTickerResp, err := instrumentsService.OptionByTicker("TT440CE3B", "SPBOPT")
	if err != nil {
		logger.Error(err.Error())
	} else {
		option := optionByTickerResp.GetInstrument()
		fmt.Printf("option name = %v, asset size = %v\n", option.GetName(), option.GetBasicAssetSize().ToFloat())
	}

	dividentsResp, err := instrumentsService.GetDividents("6afa6f80-03a7-4d83-9cf0-c19d7d021f76", time.Now(), time.Now().Add(1000*time.Hour))
	if err != nil {
		logger.Error(err.Error())
		fmt.Printf("header msg = %v\n", dividentsResp.GetHeader().Get("message"))
	} else {
		divs := dividentsResp.GetDividends()
		for i, div := range divs {
			fmt.Printf("divident %v, declared date = %v\n", i, div.GetDeclaredDate().AsTime().String())
		}
	}
}
