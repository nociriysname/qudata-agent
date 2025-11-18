package types

import "github.com/nociriysname/qudata-agent/internal/attestation"

type InstanceState struct {
	InstanceID     string            `json:"instance_id"`
	ContainerID    string            `json:"container_id"`
	Status         string            `json:"status"`
	LuksDevicePath string            `json:"luks_device_path"`
	LuksMapperName string            `json:"luks_mapper_name"`
	MountPoint     string            `json:"mount_point"`
	AllocatedPorts map[string]string `json:"allocated_ports"`
	PciAddress     string            `json:"pci_address,omitempty"`
	OriginalDriver string            `json:"original_driver,omitempty"`
}

type CreateInstanceRequest struct {
	Image          string            `json:"image"`
	ImageTag       string            `json:"image_tag"`
	StorageGB      int               `json:"storage_gb"`
	EnvVariables   map[string]string `json:"env_variables"`
	Ports          map[string]string `json:"ports"`
	SSHEnabled     bool              `json:"ssh_enabled"`
	GPUCount       int               `json:"gpu_count"`
	IsConfidential bool              `json:"is_confidential"`
}

type InitAgentRequest struct {
	AgentID     string `json:"agent_id"`
	AgentPort   int    `json:"agent_port"`
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	PID         int    `json:"pid"`
}

type AgentResponse struct {
	AgentCreated    bool   `json:"agent_created"`
	EmergencyReinit bool   `json:"emergency_reinit"`
	HostExists      bool   `json:"host_exists"`
	SecretKey       string `json:"secret_key,omitempty"`
}

type Location struct {
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
}

type CreateHostRequest struct {
	GPUName       string                        `json:"gpu_name"`
	GPUAmount     int                           `json:"gpu_amount"`
	VRAM          float64                       `json:"vram"`
	MaxCUDA       float64                       `json:"max_cuda"`
	Location      *Location                     `json:"location,omitempty"`
	Configuration attestation.ConfigurationData `json:"configuration"`
}

type InstanceAction string

const (
	ActionStart   InstanceAction = "start"
	ActionStop    InstanceAction = "stop"
	ActionRestart InstanceAction = "restart"
)

type ManageInstanceRequest struct {
	Action InstanceAction `json:"action"`
}

type StatsRequest struct {
	GPUUtil float64 `json:"gpu_util"`
	CPUUtil float64 `json:"cpu_util"`
	RAMUtil float64 `json:"ram_util"`
	MemUtil float64 `json:"mem_util"`
	InetIn  int     `json:"inet_in"`
	InetOut int     `json:"inet_out"`
	Status  string  `json:"status"`
}
