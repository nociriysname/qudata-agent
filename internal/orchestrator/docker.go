package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	agenttypes "github.com/nociriysname/qudata-agent/pkg/types"
)

const (
	kataRuntime       = "kata-qemu"
	containerDataPath = "/data"
)

func runContainer(ctx context.Context, cli *client.Client, req *agenttypes.CreateInstanceRequest, state *agenttypes.InstanceState) (string, error) {
	imageName := fmt.Sprintf("%s:%s", req.Image, req.ImageTag)

	reader, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()
	io.Copy(os.Stdout, reader)

	var envs []string
	for k, v := range req.EnvVariables {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}

	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}
	for containerPort, hostPort := range req.Ports {
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			return "", fmt.Errorf("invalid container port %s: %w", containerPort, err)
		}
		exposedPorts[port] = struct{}{}
		portBindings[port] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: hostPort,
			},
		}
	}

	containerConfig := &container.Config{
		Image:        imageName,
		Env:          envs,
		ExposedPorts: exposedPorts,
	}

	hostConfig := &container.HostConfig{
		Runtime: kataRuntime,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: state.MountPoint,
				Target: containerDataPath,
			},
		},
		PortBindings: portBindings,
	}

	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container %s: %w", resp.ID, err)
	}

	return resp.ID, nil
}

func removeContainer(ctx context.Context, cli *client.Client, containerID string) error {
	if containerID == "" {
		return nil
	}

	timeoutInSeconds := 10
	stopOptions := container.StopOptions{Timeout: &timeoutInSeconds}
	err := cli.ContainerStop(ctx, containerID, stopOptions)
	if err != nil && !client.IsErrNotFound(err) {
		fmt.Printf("Warning: failed to stop container %s: %v\n", containerID, err)
	}

	err = cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to remove container %s: %w", containerID, err)
	}

	return nil
}
