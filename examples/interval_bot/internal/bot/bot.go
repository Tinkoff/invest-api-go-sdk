package bot

import (
	"context"
	"github.com/tinkoff/invest-api-go-sdk/investgo"
)

type IntervalStrategyConfig struct {
}

type Bot struct {
	StrategyConfig IntervalStrategyConfig
	Client         *investgo.Client

	ctx       context.Context
	cancelBot context.CancelFunc

	executor *Executor
}

func NewBot(ctx context.Context, client *investgo.Client, config IntervalStrategyConfig) (*Bot, error) {
	return &Bot{
		StrategyConfig: IntervalStrategyConfig{},
		Client:         nil,
		ctx:            nil,
		cancelBot:      nil,
		executor:       nil,
	}, nil
}

func (b *Bot) Run() error {
	return nil
}

func (b *Bot) Stop() {
}
