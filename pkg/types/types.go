package types

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
	Image        string            `json:"image"`
	ImageTag     string            `json:"image_tag"`
	StorageGB    int               `json:"storage_gb"`
	EnvVariables map[string]string `json:"env_variables"`
	Ports        map[string]string `json:"ports"`
	SSHEnabled   bool              `json:"ssh_enabled"`
	// IsConfidential bool `json:"is_confidential"`
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
