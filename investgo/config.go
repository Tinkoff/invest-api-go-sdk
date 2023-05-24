package investgo

import (
	yaml "gopkg.in/yaml.v3"
	"log"
	"os"
)

// Config - структура для кофигурации SDK
type Config struct {
	// EndPoint - Для работы с реальным контуром и контуром песочницы нужны разные эндпоинты
	//https://tinkoff.github.io/investAPI/url_difference/
	EndPoint string `yaml:"EndPoint"`
	// Token - Ваш токен для апи
	Token string `yaml:"APIToken"`
	// AppName - Название вашего приложения, по умолчанию = tinkoff-api-go-sdk
	AppName string `yaml:"AppName"`
	// AccountId - Если уже есть аккаунт для апи можно указать напрямую,
	//для песочницы создастся и зпишется автоматически
	AccountId string `yaml:"AccountId"`
	// DisableResourceExhaustedRetry - Если true, то сдк не пытается ретраить, после получения ошибки об исчерпывании
	// лимита запросов, если false, то сдк ждет нужное время и пытается выполнить запрос снова. По умолчанию = false
	DisableResourceExhaustedRetry bool `yaml:"DisableResourceExhaustedRetry"`
	// MaxRetries - Максимальное количество попыток переподключения, по умолчанию = 3
	MaxRetries uint `yaml:"MaxRetries"`
}

func LoadConfig(filename string) (Config, error) {
	var c Config
	input, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, err
	}
	err = yaml.Unmarshal(input, &c)
	if err != nil {
		log.Println(err)
	}
	return c, nil
}
