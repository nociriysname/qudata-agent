package orchestrator

const (
	runtimeKataQEMU = "kata-qemu"
	runtimeKataCVM  = "kata-cvm"
)

func SelectRuntime(isConfidential bool) string {
	if isConfidential {
		return runtimeKataCVM
	}
	return runtimeKataQEMU
}
