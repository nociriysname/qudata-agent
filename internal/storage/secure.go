package storage

import (
	"os"
	"path/filepath"
	"strings"
)

const secretFilePath = "/run/lib/qudata-agent/secret"

func init() {
	_ = os.MkdirAll(filepath.Dir(secretFilePath), 0755)
}

func SaveSecretKey(key string) error {
	if err := os.MkdirAll(filepath.Dir(secretFilePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(secretFilePath, []byte(key), 0600)
}

func LoadSecretKey() (string, error) {
	if _, err := os.Stat(secretFilePath); os.IsNotExist(err) {
		return "", nil
	}

	data, err := os.ReadFile(secretFilePath)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func ShredSecretFile() error {
	if _, err := os.Stat(secretFilePath); os.IsNotExist(err) {
		return nil
	}
	_ = os.WriteFile(secretFilePath, []byte("0000000000000000"), 0600)
	return os.Remove(secretFilePath)
}
