package storage

import (
	"os"
	"path/filepath"
	"strings"
)

const secretFile = "/var/lib/qudata/secret.key"

func SaveSecretKey(key string) error {
	os.MkdirAll(filepath.Dir(secretFile), 0700)
	return os.WriteFile(secretFile, []byte(key), 0600)
}

func LoadSecretKey() (string, error) {
	data, err := os.ReadFile(secretFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
