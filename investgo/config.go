package investgo

import (
	yaml "gopkg.in/yaml.v3"
	"log"
	"os"
)

// Config - структура для кофигурации SDK
type Config struct {
	ProdEndPoint    string `yaml:"ProdEndPoint"`
	SandboxEndPoint string `yaml:"SandboxEndPoint"`
	Token           string `yaml:"APIToken"`
	AppName         string `yaml:"AppName"`
	AccountId       string `yaml:"AccountId"`
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
