//go:build linux

package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nociriysname/qudata-agent/internal/utils"
)

func prepareGPUForPassthrough(ctx context.Context) (string, string, string, error) {
	pciAddress, originalDriver, err := getMainVGAController(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("could not find main VGA controller: %w", err)
	}
	if pciAddress == "" || originalDriver == "" {
		return "", "", "", fmt.Errorf("found VGA controller but missing pci address or driver")
	}

	if err := unbindFromHostDriver(ctx, pciAddress); err != nil {
		return "", "", "", fmt.Errorf("failed to unbind GPU from host driver: %w", err)
	}

	time.Sleep(1 * time.Second)

	if err := bindToVFIODriver(ctx, pciAddress); err != nil {
		_ = returnGPUToHost(context.Background(), pciAddress, originalDriver)
		return "", "", "", fmt.Errorf("failed to bind GPU to vfio-pci driver: %w", err)
	}

	iommuGroupPath, err := getIOMMUGroupPath(pciAddress)
	if err != nil {
		_ = returnGPUToHost(context.Background(), pciAddress, originalDriver)
		return "", "", "", fmt.Errorf("failed to find IOMMU group path: %w", err)
	}

	return pciAddress, originalDriver, iommuGroupPath, nil
}

func returnGPUToHost(ctx context.Context, pciAddress, originalDriver string) error {
	unbindPath := fmt.Sprintf("/sys/bus/pci/drivers/vfio-pci/unbind")
	_ = utils.RunCommand(ctx, "", "tee", unbindPath, fmt.Sprintf("0000:%s", pciAddress))

	bindPath := fmt.Sprintf("/sys/bus/pci/drivers/%s/bind", originalDriver)
	if err := utils.RunCommand(ctx, "", "tee", bindPath, fmt.Sprintf("0000:%s", pciAddress)); err != nil {
		return fmt.Errorf("failed to re-bind GPU to '%s' driver: %w", originalDriver, err)
	}

	time.Sleep(2 * time.Second)
	log.Printf("Performing hardware reset for GPU %s...", pciAddress)
	if err := utils.RunCommand(ctx, "", "nvidia-smi", "-r"); err != nil {
		log.Printf("Warning: 'nvidia-smi -r' failed, GPU state may not be clean: %v", err)
	} else {
		log.Printf("GPU hardware reset successful.")
	}

	return nil
}

func getMainVGAController(ctx context.Context) (string, string, error) {
	// Ищем сначала 3D контроллер (дискретные карты), потом VGA (может быть интегрированной).
	for _, deviceClass := range []string{"0302", "0300"} {
		output, err := utils.RunCommandGetOutput(ctx, "", "lspci", "-vmm", "-d", fmt.Sprintf("::%s", deviceClass))
		if err != nil || output == "" {
			continue
		}

		var pciAddress, currentDriver string
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "Slot:") {
				pciAddress = strings.TrimSpace(strings.Split(line, "\t")[1])
			}
			if strings.HasPrefix(line, "Driver:") {
				currentDriver = strings.TrimSpace(strings.Split(line, "\t")[1])
			}
		}
		if pciAddress != "" && currentDriver != "" {
			return pciAddress, currentDriver, nil
		}
	}
	return "", "", fmt.Errorf("no suitable VGA/3D controller found")
}

func unbindFromHostDriver(ctx context.Context, pciAddress string) error {
	unbindPath := fmt.Sprintf("/sys/bus/pci/devices/0000:%s/driver/unbind", pciAddress)
	return utils.RunCommand(ctx, fmt.Sprintf("0000:%s", pciAddress), "tee", unbindPath)
}

func bindToVFIODriver(ctx context.Context, pciAddress string) error {

	output, err := utils.RunCommandGetOutput(ctx, "", "lspci", "-n", "-s", pciAddress)
	if err != nil {
		return fmt.Errorf("could not get vendor/device IDs for %s: %w", pciAddress, err)
	}
	parts := strings.Split(output, " ")
	if len(parts) < 3 {
		return fmt.Errorf("unexpected lspci -n output: %s", output)
	}
	vendorDeviceID := strings.Replace(parts[2], ":", " ", 1)

	newIDPath := "/sys/bus/pci/drivers/vfio-pci/new_id"
	if err := utils.RunCommand(ctx, vendorDeviceID, "tee", newIDPath); err != nil && !strings.Contains(err.Error(), "File exists") {
		return fmt.Errorf("failed to add new ID to vfio-pci: %w", err)
	}

	bindPath := "/sys/bus/pci/drivers/vfio-pci/bind"
	return utils.RunCommand(ctx, fmt.Sprintf("0000:%s", pciAddress), "tee", bindPath)
}

func getIOMMUGroupPath(pciAddress string) (string, error) {
	iommuGroupLink := fmt.Sprintf("/sys/bus/pci/devices/0000:%s/iommu_group", pciAddress)
	link, err := os.Readlink(iommuGroupLink)
	if err != nil {
		return "", fmt.Errorf("cannot find IOMMU group for %s. Is IOMMU enabled?: %w", pciAddress, err)
	}

	iommuGroupID := filepath.Base(link)
	vfioDevicePath := fmt.Sprintf("/dev/vfio/%s", iommuGroupID)

	if _, err := os.Stat(vfioDevicePath); os.IsNotExist(err) {
		return "", fmt.Errorf("VFIO device path %s does not exist", vfioDevicePath)
	}

	return vfioDevicePath, nil
}
