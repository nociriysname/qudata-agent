package cfg

import (
	"fmt"
	"os"
)

type Config struct {
	APIKey string
	Port   int
}

func LoadConfig() (*Config, error) {
	apiKey := os.Getenv("QUDATA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("QUDATA_API_KEY is required")
	}

	return &Config{
		APIKey: apiKey,
		Port:   8080,
	}, nil
}
