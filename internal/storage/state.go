package storage

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/nociriysname/qudata-agent/pkg/types"
)

const (
	stateFilePath   = "state.json"
	StatusDestroyed = "destroyed"
)

var (
	currentState *types.InstanceState
	mu           sync.RWMutex
)

func LoadState() error {
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(stateFilePath); os.IsNotExist(err) {
		currentState = &types.InstanceState{Status: StatusDestroyed}
		return nil
	}

	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		currentState = &types.InstanceState{Status: StatusDestroyed}
		return nil
	}

	var state types.InstanceState
	if err := json.Unmarshal(data, &state); err != nil {
		currentState = &types.InstanceState{Status: StatusDestroyed}
		return err
	}

	currentState = &state
	return nil
}

func SaveState(state *types.InstanceState) error {
	mu.Lock()
	defer mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(stateFilePath, data, 0600); err != nil {
		return err
	}

	currentState = state
	return nil
}

func GetState() types.InstanceState {
	mu.RLock()
	defer mu.RUnlock()

	return *currentState
}

func ClearState() error {
	mu.Lock()
	defer mu.Unlock()

	currentState = &types.InstanceState{Status: StatusDestroyed}

	err := os.Remove(stateFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
