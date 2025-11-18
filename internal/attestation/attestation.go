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

	"github.com/shirou/gopsutil/v3/host"
)

// --- Определения структур ---

type UnitValue struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type ConfigurationData struct {
	RAM            UnitValue `json:"ram,omitempty"`
	Disk           UnitValue `json:"disk,omitempty"`
	CPUName        string    `json:"cpu_name,omitempty"`
	VCPU           int       `json:"vcpu,omitempty"`
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
	VRAM          float64
	Fingerprint   string
	Configuration ConfigurationData
	CUDAVersion   float64
}

// --- Главная функция ---

func GenerateHostReport() *HostReport {
	gpus, cudaVersion, err := GetGPUInfo()
	if err != nil {
		log.Printf("Warning: Failed to get GPU info via CGO: %v", err)
	}

	var gpuName string
	var gpuAmount int
	var vram float64
	if len(gpus) > 0 {
		gpuName = gpus[0].Name
		gpuAmount = len(gpus)
		vram = gpus[0].VRAM_GB
	}

	config := getConfiguration(cudaVersion)

	return &HostReport{
		GPUName:       gpuName,
		GPUAmount:     gpuAmount,
		VRAM:          vram,
		Fingerprint:   getFingerprint(gpuName),
		CUDAVersion:   cudaVersion,
		Configuration: config,
	}
}

// --- Вспомогательные функции (адаптированы из кода Алекса) ---

func getConfiguration(cudaVersion float64) ConfigurationData {
	ram := getRAM()
	cpuCores := getCPUCores()
	cpuFreq := getCPUFreq()
	netSpeed := getNetworkSpeed()

	config := ConfigurationData{
		RAM:            ram,
		Disk:           getDisk(),
		CPUName:        getCPUName(),
		VCPU:           cpuCores, // Упрощение, vCPU = CPUCores
		CPUCores:       cpuCores,
		CPUFreq:        cpuFreq,
		MemorySpeed:    getMemorySpeed(),
		EthernetIn:     netSpeed,
		EthernetOut:    netSpeed,
		MaxCUDAVersion: cudaVersion,
	}
	config.Capacity = (float64(cpuCores) * cpuFreq * ram.Amount) / 100
	return config
}

func getFingerprint(gpuName string) string {
	info, err := host.Info()
	if err != nil {
		log.Printf("Warning: could not get host info for fingerprint: %v", err)
		return ""
	}
	var parts []string
	parts = append(parts, info.HostID)
	if gpuName != "" {
		parts = append(parts, gpuName)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func getRAM() UnitValue {
	// Реализация Алекса через /proc/meminfo
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return UnitValue{Amount: 0, Unit: "gb"}
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseFloat(fields[1], 64)
				gb := kb / 1024 / 1024
				return UnitValue{Amount: gb, Unit: "gb"}
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
	gb := float64(totalBytes) / (1024 * 1024 * 1024)
	return UnitValue{Amount: gb, Unit: "gb"}
}

func getCPUName() string {
	// Реализация Алекса
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func getCPUCores() int {
	return runtime.NumCPU()
}

func getCPUFreq() float64 {
	// Улучшенная реализация Алекса (поиск максимальной частоты)
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0.0
	}
	defer func() { _ = file.Close() }()
	var maxFreq float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
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

func getMemorySpeed() float64 {
	// Заглушка, как у Алекса
	return 2400.0
}

func getNetworkSpeed() float64 {
	// Улучшенная реализация Алекса (поиск самого быстрого физического интерфейса)
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return 1.0
	}
	var maxSpeed float64
	for _, entry := range entries {
		ifaceName := entry.Name()
		// Пропускаем виртуальные и loopback интерфейсы
		if ifaceName == "lo" || strings.HasPrefix(ifaceName, "docker") || strings.HasPrefix(ifaceName, "veth") {
			continue
		}
		// Проверяем, что интерфейс "up"
		operstatePath := "/sys/class/net/" + ifaceName + "/operstate"
		operstate, err := os.ReadFile(operstatePath)
		if err != nil || strings.TrimSpace(string(operstate)) != "up" {
			continue
		}
		// Читаем скорость
		speedPath := "/sys/class/net/" + ifaceName + "/speed"
		data, err := os.ReadFile(speedPath)
		if err != nil {
			continue
		}
		speed, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err == nil && speed > maxSpeed {
			maxSpeed = speed
		}
	}
	if maxSpeed > 0 {
		return maxSpeed / 1000 // в Gbps
	}
	return 1.0 // Default
}
