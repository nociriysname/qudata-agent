#include <nvml.h>
#include <stdlib.h>

int get_gpu_count() {
    if (nvmlInit_v2() != NVML_SUCCESS) return -1;
    unsigned int count = 0;
    nvmlReturn_t result = nvmlDeviceGetCount_v2(&count);
    nvmlShutdown();
    if (result != NVML_SUCCESS) return -1;
    return (int)count;
}

int get_gpu_info_by_index(unsigned int index, char *name, unsigned int name_len, unsigned long long *vram, double *cuda_ver) {
    if (nvmlInit_v2() != NVML_SUCCESS) return 0;

    nvmlDevice_t device;
    if (nvmlDeviceGetHandleByIndex_v2(index, &device) != NVML_SUCCESS) {
        nvmlShutdown();
        return 0;
    }

    if (nvmlDeviceGetName(device, name, name_len) != NVML_SUCCESS) {
        nvmlShutdown();
        return 0;
    }

    nvmlMemory_t memory;
    if (nvmlDeviceGetMemoryInfo(device, &memory) != NVML_SUCCESS) {
        nvmlShutdown();
        return 0;
    }
    *vram = memory.total;

    int driver_version = 0;
    if (nvmlSystemGetCudaDriverVersion(&driver_version) == NVML_SUCCESS) {
        *cuda_ver = (double)(driver_version / 1000) + (double)((driver_version % 1000) / 10) / 10.0;
    }

    nvmlShutdown();
    return 1;
}