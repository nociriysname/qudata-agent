package attestation

// HostReport содержит всю информацию о хосте, необходимую для регистрации.
type HostReport struct {
	GPUName       string            `json:"gpu_name"`
	GPUAmount     int               `json:"gpu_amount"`
	VRAM          float64           `json:"vram"`
	Fingerprint   string            `json:"fingerprint"`
	Configuration ConfigurationData `json:"configuration"`
}

// GenerateHostReport собирает полную информацию о системе.
func GenerateHostReport() *HostReport {
	return &HostReport{
		GPUName:       GetGPUName(),
		GPUAmount:     GetGPUCount(),
		VRAM:          GetVRAM(),
		Fingerprint:   GetFingerprint(),
		Configuration: GetConfiguration(), // Теперь вызываем публичную функцию
	}
}
