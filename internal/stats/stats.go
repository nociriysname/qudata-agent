package stats

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/nociriysname/qudata-agent/pkg/types"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect() types.StatsRequest {
	cpuPercent, _ := cpu.Percent(0, false)
	cpuVal := 0.0
	if len(cpuPercent) > 0 {
		cpuVal = cpuPercent[0]
	}

	vMem, _ := mem.VirtualMemory()
	ramVal := 0.0
	if vMem != nil {
		ramVal = vMem.UsedPercent
	}

	gpuStats := CollectGPUMetrics()

	inetIn := 0
	inetOut := 0

	return types.StatsRequest{
		CPUUtil: cpuVal,
		RAMUtil: ramVal,
		GPUUtil: gpuStats.GPUUtil,
		MemUtil: gpuStats.MemUtil,
		InetIn:  inetIn,
		InetOut: inetOut,
	}
}
