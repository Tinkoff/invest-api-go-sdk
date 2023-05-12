package investgo

import (
	"context"
	"crypto/tls"
	"fmt"
	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"
	"strings"
)

type ctxKey string

type Client struct {
	conn   *grpc.ClientConn
	Config Config
	Logger Logger
	ctx    context.Context
}

// NewClient - создание клиента для API Тинькофф инвестиций
func NewClient(ctx context.Context, cnf Config, l Logger) (*Client, error) {
	// default appName = invest-api-go-sdk
	if strings.Compare(cnf.AppName, "") == 0 {
		cnf.AppName = "invest-api-go-sdk"
	}

	var authKey ctxKey = "authorization"
	ctx = context.WithValue(ctx, authKey, fmt.Sprintf("Bearer %s", cnf.Token))
	ctx = metadata.AppendToOutgoingContext(ctx, "x-app-name", cnf.AppName)

	var retryPolicy = `{
		"methodConfig": [{
		  "waitForReady": true,
		  "retryPolicy": {
			  "MaxAttempts": 5,
			  "InitialBackoff": "1s",
			  "MaxBackoff": "1s",
			  "BackoffMultiplier": 1.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`
	conn, err := grpc.Dial(cnf.EndPoint,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		grpc.WithPerRPCCredentials(oauth.TokenSource{TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cnf.Token})}),
		grpc.WithDefaultServiceConfig(retryPolicy))
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		Config: cnf,
		Logger: l,
		ctx:    ctx,
	}, nil
}

type Logger interface {
	Infof(template string, args ...any)
	Errorf(template string, args ...any)
	Fatalf(template string, args ...any)
}

// NewMDStreamClient - создание клиента для сервиса стримов маркетадаты
func (c *Client) NewMDStreamClient() *MDStreamClient {
	pbClient := pb.NewMarketDataStreamServiceClient(c.conn)
	return &MDStreamClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewOrdersServiceClient - создание клиента сервиса ордеров
func (c *Client) NewOrdersServiceClient() *OrdersServiceClient {
	pbClient := pb.NewOrdersServiceClient(c.conn)
	return &OrdersServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewMarketDataServiceClient - создание клиента сервиса маркетдаты
func (c *Client) NewMarketDataServiceClient() *MarketDataServiceClient {
	pbClient := pb.NewMarketDataServiceClient(c.conn)
	return &MarketDataServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewInstrumentsServiceClient - создание клиента сервиса инструментов
func (c *Client) NewInstrumentsServiceClient() *InstrumentsServiceClient {
	pbClient := pb.NewInstrumentsServiceClient(c.conn)
	return &InstrumentsServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewUsersServiceClient - создание клиента сервиса счетов
func (c *Client) NewUsersServiceClient() *UsersServiceClient {
	pbClient := pb.NewUsersServiceClient(c.conn)
	return &UsersServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewOperationsServiceClient - создание клиента сервиса операций
func (c *Client) NewOperationsServiceClient() *OperationsServiceClient {
	pbClient := pb.NewOperationsServiceClient(c.conn)
	return &OperationsServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewStopOrdersServiceClient - создание клиента сервиса стоп-ордеров
func (c *Client) NewStopOrdersServiceClient() *StopOrdersServiceClient {
	pbClient := pb.NewStopOrdersServiceClient(c.conn)
	return &StopOrdersServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewSandboxServiceClient - создание клиента для работы с песочницей
func (c *Client) NewSandboxServiceClient() *SandboxServiceClient {
	pbClient := pb.NewSandboxServiceClient(c.conn)
	return &SandboxServiceClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewOrdersStreamClient - создание клиента стримов сделок
func (c *Client) NewOrdersStreamClient() *OrdersStreamClient {
	pbClient := pb.NewOrdersStreamServiceClient(c.conn)
	return &OrdersStreamClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// NewOperationsStreamClient - создание клиента стримов обновлений портфеля
func (c *Client) NewOperationsStreamClient() *OperationsStreamClient {
	pbClient := pb.NewOperationsStreamServiceClient(c.conn)
	return &OperationsStreamClient{
		conn:     c.conn,
		config:   c.Config,
		logger:   c.Logger,
		ctx:      c.ctx,
		pbClient: pbClient,
	}
}

// Stop - корректное завершение работы клиента
func (c *Client) Stop() error {
	// TODO add some stop options
	c.Logger.Infof("stop client")
	return c.conn.Close()
}
