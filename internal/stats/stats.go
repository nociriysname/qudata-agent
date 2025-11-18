package stats

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/nociriysname/qudata-agent/internal/utils"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Collector собирает различные метрики системы.
type Collector struct{}

// NewCollector создает новый сборщик метрик.
func NewCollector() *Collector {
	return &Collector{}
}

// GetCPUUtil возвращает загрузку CPU в процентах.
func (c *Collector) GetCPUUtil() float64 {
	percentages, err := cpu.Percent(0, false)
	if err != nil || len(percentages) == 0 {
		log.Printf("Warning: failed to get CPU utilization: %v", err)
		return 0.0
	}
	return percentages[0]
}

// GetRAMUtil возвращает использование RAM в процентах.
func (c *Collector) GetRAMUtil() float64 {
	vm, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("Warning: failed to get RAM utilization: %v", err)
		return 0.0
	}
	return vm.UsedPercent
}

// GetGPUUtil возвращает загрузку GPU в процентах.
func (c *Collector) GetGPUUtil() float64 {
	output, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
	if err != nil {
		return 0.0 // Не логируем, так как GPU может не быть
	}
	util, _ := strconv.ParseFloat(strings.TrimSpace(output), 64)
	return util
}

// GetGPUMemoryUtil возвращает использование VRAM в процентах.
func (c *Collector) GetGPUMemoryUtil() float64 {
	output, err := utils.RunCommandGetOutput(context.Background(), "", "nvidia-smi", "--query-gpu=utilization.memory", "--format=csv,noheader,nounits")
	if err != nil {
		return 0.0
	}
	util, _ := strconv.ParseFloat(strings.TrimSpace(output), 64)
	return util
}
