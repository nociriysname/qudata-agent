package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
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
	customHeaders := map[string]string{
		"X-Qudata-Agent": "true",
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation(), client.WithHTTPHeaders(customHeaders))
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

func (o *Orchestrator) CreateInstance(ctx context.Context, req agenttypes.CreateInstanceRequest) (*agenttypes.InstanceState, error) {
	currentState := storage.GetState()
	if currentState.Status != storage.StatusDestroyed {
		return nil, fmt.Errorf("an instance '%s' is already running", currentState.InstanceID)
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

	var iommuGroupPath string

	if req.GPUCount > 0 {
		pciAddress, originalDriver, iommuPath, err := prepareGPUForPassthrough(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare GPU for passthrough: %w", err)
		}
		newState.PciAddress = pciAddress
		newState.OriginalDriver = originalDriver
		iommuGroupPath = iommuPath
	}

	if err := createEncryptedVolume(ctx, newState, req.StorageGB); err != nil {
		if newState.PciAddress != "" {
			_ = returnGPUToHost(context.Background(), newState.PciAddress, newState.OriginalDriver)
		}
		_ = deleteEncryptedVolume(context.Background(), newState)
		return nil, fmt.Errorf("failed to create encrypted volume: %w", err)
	}

	runtimeName := SelectRuntime(req.IsConfidential)
	containerID, err := runContainer(ctx, o.dockerCli, &req, newState, iommuGroupPath, runtimeName)
	if err != nil {
		if newState.PciAddress != "" {
			_ = returnGPUToHost(context.Background(), newState.PciAddress, newState.OriginalDriver)
		}
		_ = deleteEncryptedVolume(context.Background(), newState)
		return nil, fmt.Errorf("failed to run container: %w", err)
	}
	newState.ContainerID = containerID

	containerIP, err := getContainerIP(ctx, o.dockerCli, containerID)
	if err != nil {
		_ = removeContainer(context.Background(), o.dockerCli, containerID)
		_ = deleteEncryptedVolume(context.Background(), newState)
		if newState.PciAddress != "" {
			_ = returnGPUToHost(context.Background(), newState.PciAddress, newState.OriginalDriver)
		}
		return nil, fmt.Errorf("failed to get container IP for network isolation: %w", err)
	}
	if err := applyNetworkIsolation(ctx, containerIP); err != nil {
		_ = removeContainer(context.Background(), o.dockerCli, containerID)
		_ = deleteEncryptedVolume(context.Background(), newState)
		if newState.PciAddress != "" {
			_ = returnGPUToHost(context.Background(), newState.PciAddress, newState.OriginalDriver)
		}
		return nil, fmt.Errorf("failed to apply network isolation: %w", err)
	}

	newState.Status = "running"
	if err := storage.SaveState(newState); err != nil {
		_ = removeContainer(context.Background(), o.dockerCli, containerID)
		_ = deleteEncryptedVolume(context.Background(), newState)
		if newState.PciAddress != "" {
			_ = returnGPUToHost(context.Background(), newState.PciAddress, newState.OriginalDriver)
		}
		return nil, fmt.Errorf("CRITICAL: failed to save state after instance creation: %w", err)
	}

	if req.SSHEnabled {
		go setupSSHInContainer(o.dockerCli, newState.ContainerID)
	}

	return newState, nil
}

func (o *Orchestrator) DeleteInstance(ctx context.Context) error {
	currentState := storage.GetState()
	if currentState.Status == storage.StatusDestroyed {
		return nil
	}

	containerIP, err := getContainerIP(ctx, o.dockerCli, currentState.ContainerID)
	if err != nil {
		fmt.Printf("Warning: could not get container IP for cleanup: %v\n", err)
	}

	if err := removeContainer(ctx, o.dockerCli, currentState.ContainerID); err != nil {
		fmt.Printf("Warning: failed to remove container during deletion: %v\n", err)
	}

	if containerIP != "" {
		if err := removeNetworkIsolation(ctx, containerIP); err != nil {
			fmt.Printf("Warning: failed to remove network isolation: %v\n", err)
		}
	}

	if currentState.PciAddress != "" && currentState.OriginalDriver != "" {
		if err := returnGPUToHost(ctx, currentState.PciAddress, currentState.OriginalDriver); err != nil {
			fmt.Printf("Warning: failed to return GPU to host: %v\n", err)
		}
	}

	if err := deleteEncryptedVolume(ctx, &currentState); err != nil {
		return fmt.Errorf("failed to delete encrypted volume: %w", err)
	}

	return storage.ClearState()
}

func (o *Orchestrator) ManageInstance(ctx context.Context, action agenttypes.InstanceAction) error {
	currentState := storage.GetState()
	if currentState.Status == storage.StatusDestroyed || currentState.ContainerID == "" {
		return fmt.Errorf("no active instance to manage")
	}

	var err error
	newStatus := currentState.Status

	timeout := 10
	stopOptions := container.StopOptions{Timeout: &timeout}

	switch action {
	case agenttypes.ActionStart:
		if currentState.Status != "paused" {
			return fmt.Errorf("instance is not stopped, current status: %s", currentState.Status)
		}
		err = o.dockerCli.ContainerStart(ctx, currentState.ContainerID, container.StartOptions{})
		if err == nil {
			newStatus = "running"
		}
	case agenttypes.ActionStop:
		err = o.dockerCli.ContainerStop(ctx, currentState.ContainerID, stopOptions)
		if err == nil {
			newStatus = "paused"
		}
	case agenttypes.ActionRestart:
		err = o.dockerCli.ContainerRestart(ctx, currentState.ContainerID, stopOptions)
		if err == nil {
			newStatus = "running"
		}
	default:
		return fmt.Errorf("unknown instance action: %s", action)
	}

	if err != nil {
		return fmt.Errorf("failed to perform action '%s': %w", action, err)
	}

	if newStatus != currentState.Status {
		currentState.Status = newStatus
		if err := storage.SaveState(&currentState); err != nil {
			log.Printf("CRITICAL: failed to save state after action '%s': %v", action, err)
		}
	}

	return nil
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

func NewLite() (*Orchestrator, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Orchestrator{dockerCli: cli}, nil
}

func (o *Orchestrator) SyncState(ctx context.Context) error {
	currentState := storage.GetState()
	if currentState.Status == storage.StatusDestroyed || currentState.ContainerID == "" {
		return nil
	}

	log.Println("Syncing agent state with Docker...")

	inspect, err := o.dockerCli.ContainerInspect(ctx, currentState.ContainerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			log.Printf("SyncState: Container %s not found. Clearing state.", currentState.ContainerID[:12])
			_ = o.DeleteInstance(ctx)
			return storage.ClearState()
		}
		return fmt.Errorf("SyncState: failed to inspect container %s: %w", currentState.ContainerID, err)
	}

	dockerStatus := inspect.State.Status // "running", "exited", "paused", etc.
	agentStatus := currentState.Status
	needsSave := false

	log.Printf("SyncState: Container status in Docker is '%s', agent state is '%s'.", dockerStatus, agentStatus)

	switch dockerStatus {
	case "running":
		if agentStatus != "running" {
			currentState.Status = "running"
			needsSave = true
		}
	case "exited", "dead":
		if agentStatus != "paused" {
			currentState.Status = "paused"
			needsSave = true
		}
	case "paused":
		if agentStatus != "paused" {
			currentState.Status = "paused"
			needsSave = true
		}
	}

	if needsSave {
		log.Printf("SyncState: State mismatch detected. Updating agent state to '%s'.", currentState.Status)
		return storage.SaveState(&currentState)
	}

	log.Println("SyncState: State is consistent.")
	return nil
}

func (o *Orchestrator) GetInstanceLogs(ctx context.Context) (string, error) {
	currentState := storage.GetState()
	if currentState.Status == storage.StatusDestroyed || currentState.ContainerID == "" {
		return "", fmt.Errorf("no active instance to get logs from")
	}

	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "100",
	}

	reader, err := o.dockerCli.ContainerLogs(ctx, currentState.ContainerID, logOptions)
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", fmt.Errorf("failed to read logs from stream: %w", err)
	}

	return buf.String(), nil
}
