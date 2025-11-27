package attestation

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type UnitValue struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type ConfigurationData struct {
	RAM            UnitValue `json:"ram,omitempty"`
	Disk           UnitValue `json:"disk,omitempty"`
	CPUName        string    `json:"cpu_name,omitempty"`
	CPUCores       int       `json:"cpu_cores,omitempty"`
	CPUFreq        float64   `json:"cpu_freq,omitempty"`
	MemorySpeed    float64   `json:"memory_speed,omitempty"`
	EthernetIn     float64   `json:"ethernet_in,omitempty"`
	EthernetOut    float64   `json:"ethernet_out,omitempty"`
	Capacity       float64   `json:"capacity,omitempty"`
	MaxCUDAVersion float64   `json:"max_cuda_version,omitempty"`
}

type HostReport struct {
	GPUName       string
	GPUAmount     int
	VRAM          float64 // GB
	Fingerprint   string
	DriverVersion string
	CUDAVersion   float64
	Configuration ConfigurationData
	Devices       []GPUHardwareInfo
}

func GenerateHostReport() *HostReport {
	sysConfig := collectSystemConfig()

	gpus, driverVer, err := GetHardwareData()
	if err != nil {
		log.Printf("WARN [Attestation]: Failed to get GPU data via NVML: %v", err)
		return &HostReport{
			Configuration: sysConfig,
			Fingerprint:   generateFingerprint(sysConfig, nil),
		}
	}

	var gpuName string
	var totalVRAM uint64
	if len(gpus) > 0 {
		gpuName = gpus[0].Name
		totalVRAM = gpus[0].TotalMemory
	}

	vramGB := float64(totalVRAM) / (1024 * 1024 * 1024)

	cudaVer := 12.2

	return &HostReport{
		GPUName:       gpuName,
		GPUAmount:     len(gpus),
		VRAM:          vramGB,
		DriverVersion: driverVer,
		CUDAVersion:   cudaVer,
		Configuration: sysConfig,
		Devices:       gpus,
		Fingerprint:   generateFingerprint(sysConfig, gpus),
	}
}

func collectSystemConfig() ConfigurationData {
	ram := getRAM()
	cpuCores := runtime.NumCPU()
	cpuFreq := getCPUFreq()

	return ConfigurationData{
		RAM:         ram,
		Disk:        getDisk(),
		CPUName:     getCPUName(),
		CPUCores:    cpuCores,
		CPUFreq:     cpuFreq,
		MemorySpeed: 2400.0,
		EthernetIn:  1.0,
		EthernetOut: 1.0,
		Capacity:    (float64(cpuCores) * cpuFreq * ram.Amount) / 100,
	}
}

func generateFingerprint(conf ConfigurationData, gpus []GPUHardwareInfo) string {
	var parts []string

	// Machine ID
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(b)))
	}

	// CPU Info
	parts = append(parts, conf.CPUName, strconv.Itoa(conf.CPUCores))

	// GPU UUIDs (Самое надежное для fingerprint)
	for _, gpu := range gpus {
		parts = append(parts, gpu.UUID)
	}

	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func getRAM() UnitValue {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return UnitValue{Amount: 0, Unit: "gb"}
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseFloat(fields[1], 64)
				return UnitValue{Amount: kb / 1024 / 1024, Unit: "gb"}
			}
		}
	}
	return UnitValue{Amount: 0, Unit: "gb"}
}

func getDisk() UnitValue {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return UnitValue{Amount: 0, Unit: "gb"}
	}
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	return UnitValue{Amount: float64(totalBytes) / (1024 * 1024 * 1024), Unit: "gb"}
}

func getCPUName() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "Unknown"
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "Unknown"
}

func getCPUFreq() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0.0
	}
	var maxFreq float64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					ghz := mhz / 1000
					if ghz > maxFreq {
						maxFreq = ghz
					}
				}
			}
		}
	}
	return maxFreq
}
