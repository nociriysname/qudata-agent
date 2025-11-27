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

// PrepareGPU находит первую NVIDIA карту, отвязывает от драйвера nvidia и привязывает к vfio-pci
func PrepareGPU(ctx context.Context) (string, string, string, error) {
	// 1. Ищем адрес GPU (0300/0302 класс)
	out, err := utils.RunCommandGetOutput(ctx, "", "sh", "-c", "lspci -D -nn | grep -i nvidia | grep -E '0300|0302' | head -n 1")
	if err != nil || len(out) == 0 {
		return "", "", "", fmt.Errorf("no NVIDIA GPU found")
	}
	// Пример: 0000:01:00.0 3D controller...
	pciAddr := strings.Fields(out)[0]

	// 2. Получаем ID устройства (Vendor:Device) для драйвера vfio
	idOut, _ := utils.RunCommandGetOutput(ctx, "", "lspci", "-n", "-s", pciAddr)
	// Пример: 0000:01:00.0 0300: 10de:2230 ... -> берем 10de:2230
	parts := strings.Fields(idOut)
	vendorDev := ""
	for _, p := range parts {
		if strings.Contains(p, ":") && len(p) == 9 {
			vendorDev = strings.Replace(p, ":", " ", 1) // 10de 2230
			break
		}
	}

	// 3. Проверяем текущий драйвер
	driverPath := fmt.Sprintf("/sys/bus/pci/devices/%s/driver", pciAddr)
	link, err := os.Readlink(driverPath)
	originalDriver := "nvidia" // предположение по умолчанию

	if err == nil {
		originalDriver = filepath.Base(link)
		if originalDriver != "vfio-pci" {
			log.Printf("[GPU] Unbinding %s from %s...", pciAddr, originalDriver)
			// Отвязываем от текущего драйвера
			unbindPath := filepath.Join(driverPath, "unbind")
			if err := os.WriteFile(unbindPath, []byte(pciAddr), 0200); err != nil {
				return "", "", "", fmt.Errorf("failed to unbind: %w", err)
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[GPU] %s is already bound to vfio-pci", pciAddr)
		}
	}

	// 4. Привязываем к VFIO
	// Загружаем модуль ядра
	utils.RunCommand(ctx, "", "modprobe", "vfio-pci")

	// Сообщаем драйверу новый ID (игнорируем ошибку, если уже есть)
	os.WriteFile("/sys/bus/pci/drivers/vfio-pci/new_id", []byte(vendorDev), 0200)

	// Привязываем устройство (игнорируем ошибку, если уже привязано)
	bindPath := "/sys/bus/pci/drivers/vfio-pci/bind"
	if _, err := os.Stat(driverPath); os.IsNotExist(err) {
		if err := os.WriteFile(bindPath, []byte(pciAddr), 0200); err != nil {
			return "", "", "", fmt.Errorf("failed to bind to vfio-pci: %w", err)
		}
	}

	// 5. Ищем группу IOMMU (На реальном ПК она ОБЯЗАНА быть)
	iommuLink := fmt.Sprintf("/sys/bus/pci/devices/%s/iommu_group", pciAddr)
	groupLink, err := os.Readlink(iommuLink)
	if err != nil {
		return "", "", "", fmt.Errorf("IOMMU group not found (Check BIOS VT-d/AMD-Vi settings!): %v", err)
	}

	groupID := filepath.Base(groupLink)
	vfioPath := fmt.Sprintf("/dev/vfio/%s", groupID)

	// Ждем появления файла (udev может тупить пару миллисекунд)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(vfioPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := os.Stat(vfioPath); os.IsNotExist(err) {
		return "", "", "", fmt.Errorf("device file %s did not appear. Is IOMMU enabled?", vfioPath)
	}

	log.Printf("[GPU] Ready for passthrough: %s (Group %s)", pciAddr, groupID)
	return pciAddr, originalDriver, vfioPath, nil
}

// ReturnGPUToHost возвращает карту обратно
func ReturnGPUToHost(ctx context.Context, pciAddr, originalDriver string) error {
	log.Printf("[GPU] Returning %s to %s...", pciAddr, originalDriver)

	// Unbind from vfio-pci
	os.WriteFile("/sys/bus/pci/drivers/vfio-pci/unbind", []byte(pciAddr), 0200)

	// Bind to original
	bindPath := fmt.Sprintf("/sys/bus/pci/drivers/%s/bind", originalDriver)
	if err := os.WriteFile(bindPath, []byte(pciAddr), 0200); err != nil {
		log.Printf("Warning: failed to rebind to %s: %v", originalDriver, err)
		// Попытка сброса, чтобы видеокарта "очнулась"
		utils.RunCommand(ctx, "", "nvidia-smi", "-r", "-i", pciAddr)
	}
	return nil
}
