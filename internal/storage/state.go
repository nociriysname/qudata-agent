package storage

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/nociriysname/qudata-agent/pkg/types"
)

const stateFile = "/var/lib/qudata/state.json"

var (
	currentState types.InstanceState
	mu           sync.RWMutex
)

func init() {
	currentState.Status = "destroyed"
}

func LoadState() error {
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			currentState = types.InstanceState{Status: "destroyed"}
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &currentState)
}

func GetState() types.InstanceState {
	mu.RLock()
	defer mu.RUnlock()
	return currentState
}

func SaveState(state *types.InstanceState) error {
	mu.Lock()
	defer mu.Unlock()
	currentState = *state

	data, _ := json.MarshalIndent(state, "", "  ")
	return os.WriteFile(stateFile, data, 0644)
}

func ClearState() error {
	mu.Lock()
	defer mu.Unlock()
	currentState = types.InstanceState{Status: "destroyed"}
	return os.Remove(stateFile)
}
