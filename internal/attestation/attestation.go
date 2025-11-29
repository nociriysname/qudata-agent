package attestation

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/nociriysname/qudata-agent/internal/utils"
	_ "github.com/shirou/gopsutil/v3/host"
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
	VRAM          float64
	Fingerprint   string
	Configuration ConfigurationData
	CUDAVersion   float64
}

type GPUInfo struct {
	Name    string
	VRAM_GB float64
}

func GenerateHostReport() *HostReport {
	gpus, cudaVersion, _ := GetGPUInfo()

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
		Fingerprint:   GetFingerprint(),
		CUDAVersion:   cudaVersion,
		Configuration: config,
	}
}

func GetGPUInfo() (gpus []GPUInfo, cudaVersion float64, err error) {
	_, err = utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi")
	if err != nil {
		return nil, 0, nil
	}

	countOutput, _ := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=count", "--format=csv,noheader")
	count, _ := strconv.Atoi(strings.TrimSpace(countOutput))
	if count == 0 {
		return nil, 0, nil
	}

	for i := 0; i < count; i++ {
		index := strconv.Itoa(i)
		var gpu GPUInfo
		nameOutput, _ := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=gpu_name", "--format=csv,noheader", "-i", index)
		gpu.Name = strings.TrimSpace(nameOutput)
		vramOutput, _ := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits", "-i", index)
		vramMiB, _ := strconv.ParseFloat(strings.TrimSpace(vramOutput), 64)
		gpu.VRAM_GB = vramMiB / 1024
		gpus = append(gpus, gpu)
	}

	fullOutput, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi")
	if err == nil {
		lines := strings.Split(fullOutput, "\n")
		for _, line := range lines {
			if strings.Contains(line, "CUDA Version:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					cudaVersion, _ = strconv.ParseFloat(fields[len(fields)-2], 64)
					break
				}
			}
		}
	} else {
		log.Printf("Warning: could not get CUDA version via nvidia-smi: %v", err)
	}

	return gpus, cudaVersion, nil
}

func GetFingerprint() string {
	var parts []string
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(b)))
	}
	serial, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=serial", "--format=csv,noheader", "-i", "0")
	if err == nil && strings.TrimSpace(serial) != "[N/A]" {
		parts = append(parts, strings.TrimSpace(serial))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func getConfiguration(cudaVersion float64) ConfigurationData {
	ram := getRAM()
	cpuCores := getCPUCores()
	cpuFreq := getCPUFreq()
	netSpeed := getNetworkSpeed()
	config := ConfigurationData{
		RAM:            ram,
		Disk:           getDisk(),
		CPUName:        getCPUName(),
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

func getRAM() UnitValue {
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
	return 2400.0
}

func getNetworkSpeed() float64 {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return 1.0
	}
	var maxSpeed float64
	for _, entry := range entries {
		ifaceName := entry.Name()
		if ifaceName == "lo" || strings.HasPrefix(ifaceName, "docker") || strings.HasPrefix(ifaceName, "veth") {
			continue
		}
		operstatePath := "/sys/class/net/" + ifaceName + "/operstate"
		operstate, err := os.ReadFile(operstatePath)
		if err != nil || strings.TrimSpace(string(operstate)) != "up" {
			continue
		}
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
		return maxSpeed / 1000
	}
	return 1.0
}
