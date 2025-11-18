//go:build linux && cgo

package attestation

/*
#cgo LDFLAGS: -lnvidia-ml

int get_gpu_count();
int get_gpu_info_by_index(unsigned int index, char *name, unsigned int name_len, unsigned long long *vram, double *cuda_ver);
*/
import "C"

type GPUInfo struct {
	Name    string
	VRAM_GB float64
}

func GetGPUInfo() (gpus []GPUInfo, cudaVersion float64, err error) {
	count := int(C.get_gpu_count())
	if count <= 0 {
		return nil, 0, nil
	}

	for i := 0; i < count; i++ {
		var name [128]C.char
		var vram C.ulonglong
		var cudaVer C.double

		result := C.get_gpu_info_by_index(C.uint(i), &name[0], C.uint(len(name)), &vram, &cudaVer)
		if result == 1 {
			gpus = append(gpus, GPUInfo{
				Name:    C.GoString(&name[0]),
				VRAM_GB: float64(vram) / (1024 * 1024 * 1024),
			})
			cudaVersion = float64(cudaVer)
		}
	}
	return gpus, cudaVersion, nil
}
