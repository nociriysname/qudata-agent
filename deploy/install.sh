#!/bin/bash
set -e

KATA_VERSION="3.12.0"
GO_VERSION="1.25.0"
INSTALL_DIR="/opt/qudata-agent"

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo ""
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   QuData Agent Installer (Static)      ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"

# 1. Проверка прав
if [ "$EUID" -ne 0 ]; then echo -e "${RED}Error: Run as root!${NC}"; exit 1; fi

# 2. Проверка IOMMU
echo -e "${YELLOW}[0/7] Checking Hardware Virtualization...${NC}"
if grep -q "iommu=on" /proc/cmdline || grep -q "amd_iommu=on" /proc/cmdline || grep -q "intel_iommu=on" /proc/cmdline; then
    echo -e "${GREEN}✓ IOMMU enabled.${NC}"
else
    echo -e "${RED}WARNING: IOMMU not found in GRUB!${NC}"
    echo "GPU Passthrough will not work. Enable 'intel_iommu=on' or 'amd_iommu=on' in /etc/default/grub."
    sleep 3
fi

# 3. Зависимости (Runtime + CGO Build)
echo -e "${YELLOW}[1/7] Installing Dependencies...${NC}"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y --no-install-recommends \
    curl wget gnupg lsb-release build-essential git pkg-config \
    cryptsetup auditd apparmor-utils nano sudo ca-certificates \
    jq tar xz-utils ubuntu-drivers-common \
    nvidia-cuda-toolkit \
    2>&1 | grep -v "^Reading\|^Building" || true

# 4. Docker
echo -e "${YELLOW}[2/7] Installing Docker...${NC}"
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker

# 5. NVIDIA Drivers
echo -e "${YELLOW}[3/7] Checking NVIDIA...${NC}"
if ! command -v nvidia-smi &> /dev/null; then
    echo "Installing drivers..."
    ubuntu-drivers autoinstall
    echo -e "${YELLOW}Drivers installed. REBOOT REQUIRED.${NC}"
    exit 0
fi
# Container Toolkit
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    tee /etc/apt/sources.list.d/nvidia-container-toolkit.list > /dev/null
apt-get update -qq
apt-get install -y nvidia-container-toolkit
nvidia-ctk runtime configure --runtime=docker
systemctl restart docker

# 6. Go
echo -e "${YELLOW}[4/7] Installing Go ${GO_VERSION}...${NC}"
rm -rf /usr/local/go
wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
tar -C /usr/local -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz
export PATH=$PATH:/usr/local/go/bin

# 7. KATA CONTAINERS
echo -e "${YELLOW}[5/7] Installing Kata Containers v${KATA_VERSION}...${NC}"

rm -rf /opt/kata /usr/bin/kata-runtime /usr/bin/containerd-shim-kata-v2

KATA_URL="https://github.com/kata-containers/kata-containers/releases/download/${KATA_VERSION}/kata-static-${KATA_VERSION}-amd64.tar.xz"
echo "Downloading..."
wget -q "$KATA_URL" -O /tmp/kata.tar.xz

tar -xJf /tmp/kata.tar.xz -C /
rm /tmp/kata.tar.xz

ln -sf /opt/kata/bin/kata-runtime /usr/local/bin/kata-runtime
ln -sf /opt/kata/bin/kata-runtime /usr/bin/kata-runtime

ln -sf /opt/kata/bin/containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-v2
ln -sf /opt/kata/bin/containerd-shim-kata-v2 /usr/bin/containerd-shim-kata-v2

# Virtiofsd
if [ -f "/opt/kata/libexec/virtiofsd" ]; then
    ln -sf /opt/kata/libexec/virtiofsd /opt/kata/bin/virtiofsd
fi

# 8. Конфигурация
echo -e "${YELLOW}[6/7] Configuring...${NC}"
mkdir -p /etc/kata-containers

if [ -f "deploy/kata-configuration.toml" ]; then
    cp "deploy/kata-configuration.toml" "/etc/kata-containers/configuration.toml"
    cp "deploy/kata-configuration-cvm.toml" "/etc/kata-containers/configuration-cvm.toml"
else
    echo "Using default Kata config..."
    /opt/kata/bin/kata-runtime --kata-config /dev/null kata-env > /etc/kata-containers/configuration.toml
fi

# Docker Config
mkdir -p /etc/docker
cat > /etc/docker/daemon.json <<EOF
{
  "runtimes": {
    "kata-qemu": {
      "path": "/opt/kata/bin/kata-runtime",
      "runtimeArgs": ["--config", "/etc/kata-containers/configuration.toml"]
    },
    "kata-cvm": {
      "path": "/opt/kata/bin/kata-runtime",
      "runtimeArgs": ["--config", "/etc/kata-containers/configuration-cvm.toml"]
    }
  }
}
EOF
systemctl restart docker
echo -e "${GREEN}✓ Kata installed and linked${NC}"

# 9. Сборка Агента
echo -e "${YELLOW}[7/7] Building QuData Agent...${NC}"
go mod tidy
CGO_ENABLED=1 go build -ldflags="-s -w" -o /usr/local/bin/qudata-agent ./cmd/agent
chmod +x /usr/local/bin/qudata-agent
mkdir -p "$INSTALL_DIR"

# Auditd
tee "/etc/audit/rules.d/99-qudata.rules" > /dev/null <<EOF
-w /usr/bin/virsh -p x -k qudata_exec_watch
-w /usr/bin/qemu-img -p x -k qudata_exec_watch
EOF
augenrules --load 2>/dev/null || true

# AppArmor
if [ -f "deploy/qudata-agent.profile" ]; then
    cp "deploy/qudata-agent.profile" "/etc/apparmor.d/usr.local.bin.qudata-agent"
    apparmor_parser -r "/etc/apparmor.d/usr.local.bin.qudata-agent"
    aa-complain "qudata-agent" 2>/dev/null || true
fi

# Systemd
cp "deploy/qudata-agent.service" /etc/systemd/system/qudata-agent.service
sed -i "s/YOUR_API_KEY_PLACEHOLDER/$API_KEY/g" /etc/systemd/system/qudata-agent.service
systemctl daemon-reload
systemctl enable --now qudata-agent.service

echo ""
echo -e "${GREEN}>>> INSTALLATION COMPLETE! <<<${NC}"
echo "Agent status: sudo systemctl status qudata-agent"