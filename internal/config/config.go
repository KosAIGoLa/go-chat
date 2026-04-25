package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Service ServiceConfig `yaml:"service"`
	HTTP    HTTPConfig    `yaml:"http"`
	GRPC    GRPCConfig    `yaml:"grpc"`
	Redis   RedisConfig   `yaml:"redis"`
	Kafka   KafkaConfig   `yaml:"kafka"`
	MySQL   MySQLConfig   `yaml:"mysql"`
	Auth    AuthConfig    `yaml:"auth"`
}

type ServiceConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
}

type HTTPConfig struct {
	Addr string `yaml:"addr"`
}
type GRPCConfig struct {
	Addr string `yaml:"addr"`
}
type RedisConfig struct {
	Addrs []string `yaml:"addrs"`
}
type KafkaConfig struct {
	Brokers []string `yaml:"brokers"`
}
type MySQLConfig struct {
	DSN string `yaml:"dsn"`
}

type AuthConfig struct {
	TokenSecret string `yaml:"token_secret"`
	TokenTTL    string `yaml:"token_ttl"`
}

func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
