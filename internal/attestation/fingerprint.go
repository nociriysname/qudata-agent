//go:build linux

package attestation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/nociriysname/qudata-agent/internal/utils"
)

func GetFingerprint() string {
	var parts []string

	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(b)))
	}

	serial, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=serial", "--format=csv,noheader", "-i", "0")
	if err == nil && strings.TrimSpace(serial) != "[N/A]" {
		parts = append(parts, strings.TrimSpace(serial))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
