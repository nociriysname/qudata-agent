package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
	"github.com/nociriysname/qudata-agent/internal/utils"
)

// Стандартные приватные сети, доступ к которым нужно заблокировать.
var privateNetworks = []string{"192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12"}

func getContainerIP(ctx context.Context, cli *client.Client, containerID string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	json, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	for _, network := range json.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}

	return "", fmt.Errorf("no IP address found for container %s", containerID)
}

func applyNetworkIsolation(ctx context.Context, containerIP string) error {
	if containerIP == "" {
		return fmt.Errorf("cannot apply network isolation for empty IP")
	}

	chain := "DOCKER-USER"

	for _, network := range privateNetworks {
		args := []string{
			"-I", chain,
			"-s", containerIP,
			"-d", network,
			"-j", "REJECT",
		}
		if err := utils.RunCommand(ctx, "", "iptables", args...); err != nil && !strings.Contains(err.Error(), "rule already exists") {
			return fmt.Errorf("failed to apply iptables rule for network %s: %w", network, err)
		}
	}
	fmt.Printf("Applied network isolation for IP %s\n", containerIP)
	return nil
}

func removeNetworkIsolation(ctx context.Context, containerIP string) error {
	if containerIP == "" {
		return nil
	}

	chain := "DOCKER-USER"

	for _, network := range privateNetworks {
		args := []string{
			"-D", chain,
			"-s", containerIP,
			"-d", network,
			"-j", "REJECT",
		}

		if err := utils.RunCommand(ctx, "", "iptables", args...); err != nil && !strings.Contains(err.Error(), "does not exist") {
			fmt.Printf("Warning: failed to remove iptables rule for IP %s, network %s: %v\n", containerIP, network, err)
		}
	}
	fmt.Printf("Removed network isolation for IP %s\n", containerIP)
	return nil
}
