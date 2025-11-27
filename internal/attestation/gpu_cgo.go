//go:build linux && cgo

package attestation

/*
#cgo LDFLAGS: -lnvidia-ml
#include <nvml.h>
#include <stdlib.h>
#include <string.h>

int wrap_nvmlInit() {
    return nvmlInit() == NVML_SUCCESS;
}

int wrap_nvmlShutdown() {
    return nvmlShutdown() == NVML_SUCCESS;
}

// Количество устройств
int wrap_get_count(unsigned int *count) {
    return nvmlDeviceGetCount(count) == NVML_SUCCESS;
}

// Получение хендла устройства по индексу
int wrap_get_handle(int index, nvmlDevice_t *device) {
    return nvmlDeviceGetHandleByIndex(index, device) == NVML_SUCCESS;
}

// Получение имени
int wrap_get_name(nvmlDevice_t device, char *name, unsigned int length) {
    return nvmlDeviceGetName(device, name, length) == NVML_SUCCESS;
}

// Получение UUID
int wrap_get_uuid(nvmlDevice_t device, char *uuid, unsigned int length) {
    return nvmlDeviceGetUUID(device, uuid, length) == NVML_SUCCESS;
}

// Получение PCI Info
int wrap_get_pci_info(nvmlDevice_t device, char *pciBusId, unsigned int length) {
    nvmlPciInfo_t pciInfo;
    if (nvmlDeviceGetPciInfo(device, &pciInfo) != NVML_SUCCESS) return 0;
    strncpy(pciBusId, pciInfo.busId, length);
    return 1;
}

// Получение памяти
int wrap_get_memory(nvmlDevice_t device, unsigned long long *total) {
    nvmlMemory_t memory;
    if (nvmlDeviceGetMemoryInfo(device, &memory) != NVML_SUCCESS) return 0;
    *total = memory.total;
    return 1;
}

// Получение версии драйвера
int wrap_get_driver_version(char *version, unsigned int length) {
    return nvmlSystemGetDriverVersion(version, length) == NVML_SUCCESS;
}

// Получение версии VBIOS
int wrap_get_vbios_version(nvmlDevice_t device, char *version, unsigned int length) {
    return nvmlDeviceGetVbiosVersion(device, version, length) == NVML_SUCCESS;
}
*/
import "C"
import (
	"fmt"
)

// GPUHardwareInfo содержит точные данные от драйвера
type GPUHardwareInfo struct {
	Index        int
	Name         string
	UUID         string
	TotalMemory  uint64 // Bytes
	PciBusID     string // 0000:01:00.0
	VbiosVersion string
}

// SystemDriverInfo информация о драйвере хоста
type SystemDriverInfo struct {
	DriverVersion string
	CUDA          float64
}

func GetHardwareData() ([]GPUHardwareInfo, string, error) {
	if C.wrap_nvmlInit() == 0 {
		return nil, "", fmt.Errorf("failed to init NVML (drivers not installed or broken)")
	}
	defer C.wrap_nvmlShutdown()

	var driverBuf [80]C.char
	driverVer := ""
	if C.wrap_get_driver_version(&driverBuf[0], 80) != 0 {
		driverVer = C.GoString(&driverBuf[0])
	}

	var count C.uint
	if C.wrap_get_count(&count) == 0 {
		return nil, driverVer, fmt.Errorf("failed to get device count")
	}

	var gpus []GPUHardwareInfo

	// 4. Перебор карт
	for i := 0; i < int(count); i++ {
		var device C.nvmlDevice_t
		if C.wrap_get_handle(C.int(i), &device) == 0 {
			continue
		}

		// Name
		var nameBuf [96]C.char
		C.wrap_get_name(device, &nameBuf[0], 96)

		// UUID
		var uuidBuf [96]C.char
		C.wrap_get_uuid(device, &uuidBuf[0], 96)

		// PCI Bus ID
		var pciBuf [32]C.char
		C.wrap_get_pci_info(device, &pciBuf[0], 32)

		// Memory
		var memTotal C.ulonglong
		C.wrap_get_memory(device, &memTotal)

		// VBIOS
		var vbiosBuf [32]C.char
		C.wrap_get_vbios_version(device, &vbiosBuf[0], 32)

		gpus = append(gpus, GPUHardwareInfo{
			Index:        i,
			Name:         C.GoString(&nameBuf[0]),
			UUID:         C.GoString(&uuidBuf[0]),
			TotalMemory:  uint64(memTotal),
			PciBusID:     C.GoString(&pciBuf[0]),
			VbiosVersion: C.GoString(&vbiosBuf[0]),
		})
	}

	return gpus, driverVer, nil
}
