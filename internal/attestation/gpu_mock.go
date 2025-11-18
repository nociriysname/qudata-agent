//go:build !linux

package attestation

type GPUInfo struct {
	Name    string
	VRAM_GB float64
}

func GetGPUInfo() (gpus []GPUInfo, cudaVersion float64, err error) {
	return nil, 0.0, nil
}
