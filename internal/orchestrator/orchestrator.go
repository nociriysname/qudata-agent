package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/nociriysname/qudata-agent/internal/storage"
	agenttypes "github.com/nociriysname/qudata-agent/pkg/types"
)

const (
	storageDir = "/var/lib/qudata/storage"
	mountDir   = "/var/lib/qudata/mounts"
)

type Orchestrator struct {
	dockerCli *client.Client
}

func New() (*Orchestrator, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	if _, err := cli.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("cannot connect to docker daemon: %w", err)
	}

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage dir: %w", err)
	}
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mount dir: %w", err)
	}

	return &Orchestrator{dockerCli: cli}, nil
}

func (o *Orchestrator) CreateInstance(ctx context.Context, req agenttypes.CreateInstanceRequest) error {
	currentState := storage.GetState()
	if currentState.Status != storage.StatusDestroyed {
		return fmt.Errorf("an instance '%s' is already running", currentState.InstanceID)
	}

	instanceID := uuid.New().String()
	newState := &agenttypes.InstanceState{
		InstanceID:     instanceID,
		Status:         "pending",
		LuksDevicePath: filepath.Join(storageDir, fmt.Sprintf("%s.img", instanceID)),
		LuksMapperName: fmt.Sprintf("qudata-%s", instanceID),
		MountPoint:     filepath.Join(mountDir, instanceID),
		AllocatedPorts: req.Ports,
	}

	if err := createEncryptedVolume(ctx, newState, req.StorageGB); err != nil {
		_ = deleteEncryptedVolume(context.Background(), newState)
		return fmt.Errorf("failed to create encrypted volume: %w", err)
	}

	containerID, err := runContainer(ctx, o.dockerCli, &req, newState)
	if err != nil {
		_ = deleteEncryptedVolume(context.Background(), newState)
		return fmt.Errorf("failed to run container: %w", err)
	}

	newState.ContainerID = containerID
	newState.Status = "running"

	if err := storage.SaveState(newState); err != nil {
		_ = removeContainer(context.Background(), o.dockerCli, containerID)
		_ = deleteEncryptedVolume(context.Background(), newState)
		return fmt.Errorf("CRITICAL: failed to save state after instance creation: %w", err)
	}

	return nil
}

func (o *Orchestrator) DeleteInstance(ctx context.Context) error {
	currentState := storage.GetState()
	if currentState.Status == storage.StatusDestroyed {
		return nil
	}

	if err := removeContainer(ctx, o.dockerCli, currentState.ContainerID); err != nil {
		fmt.Printf("Warning: failed to remove container during deletion: %v\n", err)
	}

	if err := deleteEncryptedVolume(ctx, &currentState); err != nil {
		return fmt.Errorf("failed to delete encrypted volume: %w", err)
	}

	return storage.ClearState()
}

func (o *Orchestrator) AddSSHKey(ctx context.Context, publicKey string) error {
	currentState := storage.GetState()
	if currentState.Status != "running" || currentState.ContainerID == "" {
		return fmt.Errorf("no active instance to add SSH key to")
	}
	return addSSHKey(ctx, o.dockerCli, currentState.ContainerID, publicKey)
}

func (o *Orchestrator) RemoveSSHKey(ctx context.Context, publicKey string) error {
	currentState := storage.GetState()
	if currentState.Status != "running" || currentState.ContainerID == "" {
		return fmt.Errorf("no active instance to remove SSH key from")
	}
	return removeSSHKey(ctx, o.dockerCli, currentState.ContainerID, publicKey)
}

func (o *Orchestrator) ListSSHKeys(ctx context.Context) ([]string, error) {
	currentState := storage.GetState()
	if currentState.Status != "running" || currentState.ContainerID == "" {
		return nil, fmt.Errorf("no active instance to list SSH keys from")
	}

	keysString, err := listSSHKeys(ctx, o.dockerCli, currentState.ContainerID)
	if err != nil {
		return nil, err
	}

	var keys []string
	for _, key := range strings.Split(keysString, "\n") {
		if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
			keys = append(keys, trimmedKey)
		}
	}

	return keys, nil
}
