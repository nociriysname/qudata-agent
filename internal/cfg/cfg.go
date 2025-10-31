package config

import (
	"errors"
	"os"
)

type Config struct {
	APIKey string
}

func LoadConfig() (*Config, error) {
	apiKey := os.Getenv("QUDATA_API_KEY")
	if apiKey == "" {
		return nil, errors.New("QUDATA_API_KEY environment variable not set")
	}

	cfg := &Config{
		APIKey: apiKey,
	}

	return cfg, nil
}
