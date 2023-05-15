package investgo

import (
	yaml "gopkg.in/yaml.v3"
	"log"
	"os"
)

// Config - структура для кофигурации SDK
type Config struct {
	EndPoint                      string `yaml:"EndPoint"`
	Token                         string `yaml:"APIToken"`
	AppName                       string `yaml:"AppName"`
	AccountId                     string `yaml:"AccountId"`
	DisableResourceExhaustedRetry bool   `yaml:"DisableResourceExhaustedRetry"`
	MaxRetries                    uint   `yaml:"MaxRetries"`
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
