//go:build linux && cgo

package attestation

/*
#cgo LDFLAGS: -L/usr/lib/x86_64-linux-gnu -lnvidia-ml

const char* getGpuName();
*/
import "C"

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

func GetFingerprint() string {
	var parts []string

	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(b)))
	}

	if name := C.getGpuName(); name != nil {
		parts = append(parts, C.GoString(name))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
