package orchestrator

import (
	"context"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func setupSSHInContainer(cli *client.Client, containerID string) {
	log.Printf("Starting SSH setup in container %s...", containerID[:12])
	time.Sleep(5 * time.Second)

	setupCommands := [][]string{
		{"apt-get", "update", "-qq"},
		{"sh", "-c", "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq openssh-server"},
		{"mkdir", "-p", "/var/run/sshd"},
		{"sed", "-i", "s/#PermitRootLogin prohibit-password/PermitRootLogin prohibit-password/", "/etc/ssh/sshd_config"},
		{"sed", "-i", "s/PermitRootLogin yes/PermitRootLogin prohibit-password/", "/etc/ssh/sshd_config"},
		{"mkdir", "-p", "/root/.ssh"},
		{"chmod", "700", "/root/.ssh"},
		{"touch", "/root/.ssh/authorized_keys"},
		{"chmod", "600", "/root/.ssh/authorized_keys"},
	}

	ctx := context.Background()
	for _, cmd := range setupCommands {
		if err := ExecInContainer(ctx, cli, containerID, cmd); err != nil {
			log.Printf("Warning: SSH setup command failed for %s: %v", containerID[:12], err)
		}
	}

	if err := execInContainerDetached(ctx, cli, containerID, []string{"/usr/sbin/sshd", "-D"}); err != nil {
		log.Printf("ERROR: Failed to start SSH daemon in %s: %v", containerID[:12], err)
		return
	}

	log.Printf("SSH daemon started for container %s.", containerID[:12])
}

func execInContainerDetached(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
	execConfig := types.ExecConfig{
		Cmd:    cmd,
		Detach: true,
	}
	execID, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return err
	}
	return cli.ContainerExecStart(ctx, execID.ID, types.ExecStartCheck{Detach: true})
}
