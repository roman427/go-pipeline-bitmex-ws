package config

import (
	"app/workers"

	"github.com/spf13/viper"
)

type Config struct {
	Debug     bool
	LogPath   string
	Http      HttpConfig
	Db        DbConfig
	Providers Providers
}

type Providers struct {
	Bitmex workers.BitmexConfig
}

type HttpConfig struct {
	Port       int
	StaticPath string
}

type DbConfig struct {
	Name           string
	Adapter        string
	Host           string
	Port           int
	Database       string
	User           string
	Password       string
	MaxConnections int
}

func NewConfig() *Config {
	cfg := Config{}
	return &cfg
}

func (conf *Config) Load(path string) error {
	var cc Config
	viper.SetConfigType("yaml")
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	if err := viper.Unmarshal(&cc); err != nil {
		return err
	}
	*conf = cc
	return nil
}
