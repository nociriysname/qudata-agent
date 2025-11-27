//go:build linux && cgo

package stats

/*
#cgo LDFLAGS: -lnvidia-ml
#include <nvml.h>
#include <stdlib.h>

// Инициализация (можно вызывать много раз, NVML это держит)
int stats_nvml_init() {
    return nvmlInit() == NVML_SUCCESS;
}

int stats_get_handle(int index, nvmlDevice_t *device) {
    return nvmlDeviceGetHandleByIndex(index, device) == NVML_SUCCESS;
}

// Температура
int stats_get_temp(nvmlDevice_t device, unsigned int *temp) {
    return nvmlDeviceGetTemperature(device, NVML_TEMPERATURE_GPU, temp) == NVML_SUCCESS;
}

// Утилизация (GPU и Memory)
int stats_get_util(nvmlDevice_t device, unsigned int *gpu_util, unsigned int *mem_util) {
    nvmlUtilization_t util;
    if (nvmlDeviceGetUtilizationRates(device, &util) != NVML_SUCCESS) return 0;
    *gpu_util = util.gpu;
    *mem_util = util.memory;
    return 1;
}
*/
import "C"

type GPUMetrics struct {
	Index       int
	Temperature int
	GPUUtil     float64
	MemUtil     float64
}

// CollectGPUMetrics опрашивает первую видеокарту (для MVP)
func CollectGPUMetrics() GPUMetrics {
	// Пытаемся инициализировать (если уже инициализировано, не страшно)
	C.stats_nvml_init()

	// Берем 0-ю карту
	var device C.nvmlDevice_t
	if C.stats_get_handle(0, &device) == 0 {
		return GPUMetrics{}
	}

	var temp C.uint
	C.stats_get_temp(device, &temp)

	var gpuUtil, memUtil C.uint
	C.stats_get_util(device, &gpuUtil, &memUtil)

	return GPUMetrics{
		Index:       0,
		Temperature: int(temp),
		GPUUtil:     float64(gpuUtil),
		MemUtil:     float64(memUtil),
	}
}
