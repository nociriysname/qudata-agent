package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/nociriysname/qudata-agent/internal/util"
	"github.com/nociriysname/qudata-agent/pkg/types"
)

func createEncryptedVolume(ctx context.Context, state *types.InstanceState, storageGB int) error {
	dekBytes := make([]byte, 32)
	if _, err := rand.Read(dekBytes); err != nil {
		return fmt.Errorf("failed to generate DEK: %w", err)
	}
	dek := hex.EncodeToString(dekBytes)

	storageBytes := fmt.Sprintf("%dG", storageGB)
	if err := util.RunCommand(ctx, "", "truncate", "-s", storageBytes, state.LuksDevicePath); err != nil {
		return fmt.Errorf("failed to create image file: %w", err)
	}

	if err := util.RunCommand(ctx, dek, "cryptsetup", "luksFormat", "--type", "luks2", state.LuksDevicePath); err != nil {
		return fmt.Errorf("luksFormat failed: %w", err)
	}

	if err := util.RunCommand(ctx, dek, "cryptsetup", "luksOpen", state.LuksDevicePath, state.LuksMapperName); err != nil {
		return fmt.Errorf("luksOpen failed: %w", err)
	}

	mapperPath := fmt.Sprintf("/dev/mapper/%s", state.LuksMapperName)

	if err := util.RunCommand(ctx, "", "mkfs.ext4", mapperPath); err != nil {
		return fmt.Errorf("mkfs.ext4 failed: %w", err)
	}

	if err := os.MkdirAll(state.MountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	if err := util.RunCommand(ctx, "", "mount", mapperPath, state.MountPoint); err != nil {
		return fmt.Errorf("mount failed: %w", err)
	}

	return nil
}

func deleteEncryptedVolume(ctx context.Context, state *types.InstanceState) error {
	mapperPath := fmt.Sprintf("/dev/mapper/%s", state.LuksMapperName)

	if err := util.RunCommand(ctx, "", "umount", "-l", state.MountPoint); err != nil {
	}

	if err := util.RunCommand(ctx, "", "cryptsetup", "luksClose", state.LuksMapperName); err != nil {
	}

	if err := util.RunCommand(ctx, "", "shred", "-n", "1", "-z", "-u", state.LuksDevicePath); err != nil {
	}

	_ = os.Remove(state.MountPoint)

	return nil
}
