package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

const authorizedKeysPath = "/root/.ssh/authorized_keys"

func addSSHKey(ctx context.Context, cli *client.Client, containerID, publicKey string) error {
	if !strings.HasPrefix(publicKey, "ssh-") {
		return fmt.Errorf("invalid public key format")
	}

	cmd := fmt.Sprintf("mkdir -p /root/.ssh && touch %s && grep -q -F '%s' %s || echo '%s' >> %s",
		authorizedKeysPath, publicKey, authorizedKeysPath, publicKey, authorizedKeysPath)

	return execInContainer(ctx, cli, containerID, []string{"sh", "-c", cmd})
}

// removeSSHKey удаляет публичный SSH-ключ из authorized_keys контейнера.
func removeSSHKey(ctx context.Context, cli *client.Client, containerID, publicKey string) error {
	if !strings.HasPrefix(publicKey, "ssh-") {
		return fmt.Errorf("invalid public key format")
	}

	escapedKey := strings.ReplaceAll(publicKey, "/", "\\/")
	cmd := fmt.Sprintf("sed -i '/^%s$/d' %s", escapedKey, authorizedKeysPath)

	return execInContainer(ctx, cli, containerID, []string{"sh", "-c", cmd})
}

func listSSHKeys(ctx context.Context, cli *client.Client, containerID string) (string, error) {
	execID, err := cli.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		Cmd:          []string{"cat", authorizedKeysPath},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", err
	}

	resp, err := cli.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return "", err
	}
	defer resp.Close()

	var outBuf bytes.Buffer
	_, err = resp.Reader.WriteTo(&outBuf)
	if err != nil {
	}

	inspect, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return "", err
	}

	if inspect.ExitCode != 0 {
		return "", nil
	}

	return outBuf.String(), nil
}

func execInContainer(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	err = cli.ContainerExecStart(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("failed to start exec: %w", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec command failed with exit code %d", inspect.ExitCode)
	}

	return nil
}
