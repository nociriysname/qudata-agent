//go:build linux

package attestation

import (
	"context"
	"strconv"
	"strings"

	"github.com/nociriysname/qudata-agent/internal/utils"
)

// GPUInfo содержит информацию о одной видеокарте.
type GPUInfo struct {
	Name    string
	VRAM_GB float64
}

// GetGPUInfo собирает информацию обо всех GPU NVIDIA в системе через nvidia-smi.
func GetGPUInfo() (gpus []GPUInfo, cudaVersion float64, err error) {
	// Получаем количество GPU
	countOutput, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=count", "--format=csv,noheader")
	if err != nil {
		// Если nvidia-smi не найдена, это не ошибка, просто нет GPU.
		return nil, 0, nil
	}
	count, _ := strconv.Atoi(strings.TrimSpace(countOutput))
	if count == 0 {
		return nil, 0, nil
	}

	// Получаем информацию по каждой карте
	for i := 0; i < count; i++ {
		index := strconv.Itoa(i)
		var gpu GPUInfo

		// Имя
		nameOutput, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=gpu_name", "--format=csv,noheader", "-i", index)
		if err == nil {
			gpu.Name = strings.TrimSpace(nameOutput)
		}

		// VRAM
		vramOutput, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits", "-i", index)
		if err == nil {
			vramMiB, _ := strconv.ParseFloat(strings.TrimSpace(vramOutput), 64)
			gpu.VRAM_GB = vramMiB / 1024
		}
		gpus = append(gpus, gpu)
	}

	// Версия CUDA
	cudaOutput, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-driver=cuda_version", "--format=csv,noheader")
	if err == nil {
		cudaVersion, _ = strconv.ParseFloat(strings.TrimSpace(cudaOutput), 64)
	}

	return gpus, cudaVersion, nil
}
