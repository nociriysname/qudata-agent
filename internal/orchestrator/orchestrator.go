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

type QudataClient interface {
	NotifyInstanceReady(instanceID string) error
}

type Orchestrator struct {
	dockerCli *client.Client
	qudataCli QudataClient
}

func New(qClient QudataClient) (*Orchestrator, error) {
	customHeaders := map[string]string{"X-Qudata-Agent": "true"}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation(), client.WithHTTPHeaders(customHeaders))
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	if _, err := cli.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}

	os.MkdirAll(storageDir, 0755)
	os.MkdirAll(mountDir, 0755)

	return &Orchestrator{dockerCli: cli, qudataCli: qClient}, nil
}

func (o *Orchestrator) CreateInstance(ctx context.Context, req agenttypes.CreateInstanceRequest) (*agenttypes.InstanceState, error) {
	currentState := storage.GetState()
	if currentState.Status != "destroyed" && currentState.Status != "" {
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

	var deviceMappings []container.DeviceMapping
	if req.GPUCount > 0 {
		pci, origDriver, vfioPath, err := PrepareGPU(ctx)
		if err != nil {
			return nil, fmt.Errorf("GPU error: %w", err)
		}
		newState.PciAddress = pci
		newState.OriginalDriver = origDriver

		deviceMappings = []container.DeviceMapping{
			{PathOnHost: "/dev/vfio/vfio", PathInContainer: "/dev/vfio/vfio", CgroupPermissions: "rwm"},
			{PathOnHost: vfioPath, PathInContainer: vfioPath, CgroupPermissions: "rwm"},
		}
	}

	if err := createEncryptedVolume(ctx, newState, req.StorageGB); err != nil {
		o.rollback(ctx, newState)
		return nil, fmt.Errorf("LUKS error: %w", err)
	}

	runtimeName := SelectRuntime(req.IsConfidential)
	containerID, err := runContainer(ctx, o.dockerCli, &req, newState, deviceMappings, runtimeName)
	if err != nil {
		o.rollback(ctx, newState)
		return nil, fmt.Errorf("start failed: %w", err)
	}

	newState.ContainerID = containerID
	newState.Status = "running"
	storage.SaveState(newState)

	if req.SSHEnabled {
		go setupSSHInContainer(o.dockerCli, o.qudataCli, newState.ContainerID)
	}

	return newState, nil
}

func (o *Orchestrator) DeleteInstance(ctx context.Context) error {
	state := storage.GetState()
	if state.ContainerID == "" {
		return nil
	}
	o.rollback(ctx, &state)
	return nil
}

func (o *Orchestrator) rollback(ctx context.Context, state *agenttypes.InstanceState) {
	removeContainer(ctx, o.dockerCli, state.ContainerID)
	deleteEncryptedVolume(ctx, state)
	if state.PciAddress != "" {
		ReturnGPUToHost(ctx, state.PciAddress, state.OriginalDriver)
	}
	storage.ClearState()
}

func (o *Orchestrator) ManageInstance(ctx context.Context, action agenttypes.InstanceAction) error {
	state := storage.GetState()
	if state.ContainerID == "" {
		return fmt.Errorf("no active instance")
	}

	var err error
	newStatus := state.Status
	timeout := 10

	switch action {
	case agenttypes.ActionStop:
		err = o.dockerCli.ContainerStop(ctx, state.ContainerID, container.StopOptions{Timeout: &timeout})
		if err == nil {
			newStatus = "paused"
		}
	case agenttypes.ActionStart:
		err = o.dockerCli.ContainerStart(ctx, state.ContainerID, container.StartOptions{})
		if err == nil {
			newStatus = "running"
		}
	case agenttypes.ActionRestart:
		err = o.dockerCli.ContainerRestart(ctx, state.ContainerID, container.StopOptions{Timeout: &timeout})
		if err == nil {
			newStatus = "running"
		}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}

	if err != nil {
		return fmt.Errorf("manage action %s failed: %w", action, err)
	}

	if newStatus != state.Status {
		state.Status = newStatus
		storage.SaveState(&state)
	}

	return nil
}

func (o *Orchestrator) AddSSHKey(ctx context.Context, key string) error {
	state := storage.GetState()
	if state.Status != "running" {
		return fmt.Errorf("instance is not running")
	}
	return addSSHKey(ctx, o.dockerCli, state.ContainerID, key)
}

func (o *Orchestrator) RemoveSSHKey(ctx context.Context, key string) error {
	state := storage.GetState()
	if state.Status != "running" {
		return fmt.Errorf("instance is not running")
	}
	return removeSSHKey(ctx, o.dockerCli, state.ContainerID, key)
}

func (o *Orchestrator) ListSSHKeys(ctx context.Context) ([]string, error) {
	state := storage.GetState()
	if state.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}

	out, err := listSSHKeys(ctx, o.dockerCli, state.ContainerID)
	if err != nil {
		return nil, err
	}

	var keys []string
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys, nil
}

func (o *Orchestrator) GetInstanceLogs(ctx context.Context) (string, error) {
	state := storage.GetState()
	if state.ContainerID == "" {
		return "", fmt.Errorf("no instance found")
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "100",
	}

	reader, err := o.dockerCli.ContainerLogs(ctx, state.ContainerID, options)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(reader)
	return buf.String(), nil
}

func (o *Orchestrator) SyncState(ctx context.Context) error {
	currentState := storage.GetState()
	if currentState.ContainerID == "" {
		return nil
	}

	_, err := o.dockerCli.ContainerInspect(ctx, currentState.ContainerID)
	if err != nil {
		if client.IsErrNotFound(err) {

			log.Printf("SyncState: Container %s not found. Cleaning up.", currentState.ContainerID)
			o.rollback(ctx, &currentState)
			return nil
		}
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	return nil
}
