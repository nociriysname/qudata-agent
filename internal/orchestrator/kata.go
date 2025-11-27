package orchestrator

const (
	RuntimeKataQEMU = "kata-qemu"
	RuntimeKataCVM  = "kata-cvm"
)

func SelectRuntime(isConfidential bool) string {
	if isConfidential {
		return RuntimeKataCVM
	}
	return RuntimeKataQEMU
}
