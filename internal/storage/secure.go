package storage

import (
	"encoding/json"
	"os"
	"os/exec"
)

const secretFilePath = "secret.json"

type secretData struct {
	SecretKey string `json:"secret_key"`
}

func SaveSecretKey(key string) error {
	data, err := json.Marshal(secretData{SecretKey: key})
	if err != nil {
		return err
	}
	return os.WriteFile(secretFilePath, data, 0600)
}

func LoadSecretKey() (string, error) {
	if _, err := os.Stat(secretFilePath); os.IsNotExist(err) {
		return "", nil
	}

	data, err := os.ReadFile(secretFilePath)
	if err != nil {
		return "", err
	}

	var s secretData
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}

	return s.SecretKey, nil
}

func ShredSecretFile() error {
	if _, err := os.Stat(secretFilePath); os.IsNotExist(err) {
		return nil
	}

	cmd := exec.Command("shred", "-n", "1", "-z", "-u", secretFilePath)
	return cmd.Run()
}
