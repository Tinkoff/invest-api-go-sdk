package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"math/rand"
	"time"

	sdk "github.com/tinkoff/invest-api-go-sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var Instruments sdk.InstrumentsServiceClient
var MarketData sdk.MarketDataServiceClient
var Operations sdk.OperationsServiceClient
var Users sdk.UsersServiceClient

var ctx context.Context
var conn *grpc.ClientConn
var md metadata.MD

const (
	address = "invest-public-api.tinkoff.ru:443"
)

func SDKInit(token string) {
	rand.Seed(time.Now().UnixNano())
	flag.Parse()

	ctx = context.Background()

	var err error
	conn, err = grpc.Dial(address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect to grpc server: %v", err)
	}

	md = metadata.New(map[string]string{"Authorization": "Bearer " + token})
	ctx = metadata.NewOutgoingContext(ctx, md)

	Instruments = sdk.NewInstrumentsServiceClient(conn)
	MarketData = sdk.NewMarketDataServiceClient(conn)
	Operations = sdk.NewOperationsServiceClient(conn)
	Users = sdk.NewUsersServiceClient(conn)
	MarketData = sdk.NewMarketDataServiceClient(conn)
}

func GetOrderBook(figi string, depth int32) (*sdk.GetOrderBookResponse, error) {
	var or sdk.GetOrderBookRequest
	or.Figi = figi
	or.Depth = depth
	if or.Depth == 0 {
		or.Depth = 10
	}

	r, err := MarketData.GetOrderBook(ctx, &or)

	if err != nil {
		return nil, err
	}

	return r, nil
}

func GetLastPricesForAll() ([]*sdk.LastPrice, error) {
	var lr sdk.GetLastPricesRequest
	r, err := MarketData.GetLastPrices(ctx, &lr)

	if err != nil {
		return nil, err
	}

	return r.LastPrices, nil
}

func GetLastPrices(figies []string) ([]*sdk.LastPrice, error) {
	var lr sdk.GetLastPricesRequest
	lr.Figi = figies
	r, err := MarketData.GetLastPrices(ctx, &lr)

	if err != nil {
		return nil, err
	}

	return r.LastPrices, nil
}

func GetCandles(figi string, from time.Time, to time.Time, interval sdk.CandleInterval) ([]*sdk.HistoricCandle, error) {

	tsFrom := timestamppb.New(from)
	tsTo := timestamppb.New(to)

	var mr sdk.GetCandlesRequest
	mr.Figi = figi
	mr.From = tsFrom
	mr.To = tsTo
	mr.Interval = interval

	r, err := MarketData.GetCandles(ctx, &mr)

	if err != nil {
		return nil, err
	}
	return r.GetCandles(), nil
}

func GetAccounts() ([]*sdk.Account, error) {
	var ar sdk.GetAccountsRequest
	r, err := Users.GetAccounts(ctx, &ar)
	if err != nil {
		return nil, err
	}
	return r.GetAccounts(), nil
}

func WithdrawLimits(account string) (*sdk.WithdrawLimitsResponse, error) {
	var wr sdk.WithdrawLimitsRequest
	wr.AccountId = account
	r, err := Operations.GetWithdrawLimits(ctx, &wr)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func GetPositions(account string) (*sdk.PositionsResponse, error) {
	var pr sdk.PositionsRequest
	pr.AccountId = account
	r, err := Operations.GetPositions(ctx, &pr)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func GetPortfolio(account string) ([]*sdk.PortfolioPosition, error) {
	var pr sdk.PortfolioRequest
	pr.AccountId = account
	r, err := Operations.GetPortfolio(ctx, &pr)
	if err != nil {
		return nil, err
	}
	return r.GetPositions(), nil
}

func GetOperations(AccountId string, from time.Time, to time.Time, figi string) ([]*sdk.Operation, error) {
	var or sdk.OperationsRequest
	or.AccountId = AccountId

	tsFrom := timestamppb.New(from)
	tsTo := timestamppb.New(to)

	or.From = tsFrom
	or.To = tsTo
	or.Figi = figi

	r, err := Operations.GetOperations(ctx, &or)
	if err != nil {
		return nil, err
	}
	return r.GetOperations(), nil
}

func GetSharesBase() ([]*sdk.Share, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_BASE

	r, err := Instruments.Shares(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetSharesAll() ([]*sdk.Share, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_ALL

	r, err := Instruments.Shares(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetETFsBase() ([]*sdk.Etf, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_BASE

	r, err := Instruments.Etfs(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetETFsAll() ([]*sdk.Etf, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_ALL

	r, err := Instruments.Etfs(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetBondsBase() ([]*sdk.Bond, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_BASE

	r, err := Instruments.Bonds(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetBondsAll() ([]*sdk.Bond, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_ALL

	r, err := Instruments.Bonds(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetFuturesBase() ([]*sdk.Future, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_BASE

	r, err := Instruments.Futures(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}

func GetFuturesAll() ([]*sdk.Future, error) {
	var ir sdk.InstrumentsRequest
	ir.InstrumentStatus = sdk.InstrumentStatus_INSTRUMENT_STATUS_ALL

	r, err := Instruments.Futures(ctx, &ir)
	if err != nil {
		return nil, err
	}
	return r.GetInstruments(), nil
}
