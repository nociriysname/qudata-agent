package utils

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func RunCommand(ctx context.Context, stdinData string, name string, args ...string) error {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command '%s %s' failed: %w; stderr: %s",
			name, strings.Join(args, " "), err, stderr.String())
	}

	return nil
}

func RunCommandGetOutput(ctx context.Context, stdinData string, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command '%s %s' failed: %w; stderr: %s",
			name, strings.Join(args, " "), err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}
