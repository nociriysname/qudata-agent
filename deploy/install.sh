#!/bin/bash

set -e

GO_VERSION="1.25.0"
KATA_VERSION="3.12.0"
INSTALL_DIR="/opt/qudata-agent"

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo ""
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║         QuData Agent Installer         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
echo ""

# --- Проверки ---
if [ "$EUID" -ne 0 ]; then echo -e "${RED}Error: Run as root!${NC}"; exit 1; fi
if [ -z "$1" ]; then echo -e "${RED}Error: API key is required${NC}"; exit 1; fi
API_KEY="$1"

# --- Шаг 0: Проверка IOMMU  ---
echo -e "${YELLOW}[0/7] Checking Hardware Virtualization & IOMMU...${NC}"

if grep -q "iommu=on" /proc/cmdline || grep -q "amd_iommu=on" /proc/cmdline || grep -q "intel_iommu=on" /proc/cmdline; then
    echo -e "${GREEN}✓ IOMMU enabled in kernel parameters.${NC}"
else
    echo -e "${RED}WARNING: IOMMU not enabled in GRUB!${NC}"
    echo -e "${YELLOW}GPU Passthrough will FAIL without 'intel_iommu=on' or 'amd_iommu=on' in /etc/default/grub.${NC}"
    echo "Press Ctrl+C to abort and fix GRUB, or wait 5s to continue anyway..."
    sleep 5
fi

# --- Шаг 1: Обновление и зависимости ---
echo -e "${YELLOW}[1/7] Updating system and installing dependencies...${NC}"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y --no-install-recommends \
    curl wget gnupg lsb-release build-essential git pkg-config \
    cryptsetup auditd apparmor-utils nano sudo ca-certificates \
    jq tar xz-utils ubuntu-drivers-common \
    nvidia-cuda-toolkit \
    2>&1 | grep -v "^Reading\|^Building" || true

echo -e "${GREEN}✓ System dependencies installed${NC}"

# --- Шаг 2: Docker ---
echo -e "${YELLOW}[2/7] Installing Docker...${NC}"
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker
echo -e "${GREEN}✓ Docker is running${NC}"

# --- Шаг 3: Драйверы NVIDIA ---
echo -e "${YELLOW}[3/7] Checking NVIDIA drivers...${NC}"
if ! command -v nvidia-smi &> /dev/null; then
    echo "  Installing NVIDIA drivers..."
    ubuntu-drivers autoinstall
    echo -e "${YELLOW}Drivers installed. A REBOOT IS REQUIRED! Run script again after reboot.${NC}"
    exit 0
fi

# Установка Container Toolkit (runtime для обычных контейнеров, не Kata)
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    tee /etc/apt/sources.list.d/nvidia-container-toolkit.list > /dev/null
apt-get update -qq
apt-get install -y nvidia-container-toolkit
nvidia-ctk runtime configure --runtime=docker
systemctl restart docker
echo -e "${GREEN}✓ NVIDIA components ready${NC}"

# --- Шаг 4: Установка Go  ---
echo -e "${YELLOW}[4/7] Installing Go ${GO_VERSION}...${NC}"
rm -rf /usr/local/go
wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
tar -C /usr/local -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz
export PATH=$PATH:/usr/local/go/bin

# --- Шаг 5: Kata Containers  ---
echo -e "${YELLOW}[5/7] Building Kata Containers v${KATA_VERSION}...${NC}"

rm -rf /opt/kata /usr/bin/kata-runtime /usr/local/bin/kata-runtime
rm -f /usr/bin/containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-v2

git clone -b ${KATA_VERSION} https://github.com/kata-containers/kata-containers.git /tmp/kata-src
cd /tmp/kata-src/src/runtime

echo "Compiling..."
make PREFIX=/opt/kata

echo "Installing..."
make install PREFIX=/opt/kata

ln -sf /opt/kata/bin/kata-runtime /usr/local/bin/kata-runtime
ln -sf /opt/kata/bin/kata-runtime /usr/bin/kata-runtime

ln -sf /opt/kata/bin/containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-v2
ln -sf /opt/kata/bin/containerd-shim-kata-v2 /usr/bin/containerd-shim-kata-v2

cd ~/

# Virtiofsd
if [ ! -f "/opt/kata/bin/virtiofsd" ]; then
    wget -q -O /opt/kata/bin/virtiofsd https://gitlab.com/virtio-fs/virtiofsd/-/jobs/artifacts/main/raw/virtiofsd-x86_64?job=publish
    chmod +x /opt/kata/bin/virtiofsd
fi

# Конфигурация Kata
mkdir -p /etc/kata-containers
cp "deploy/kata-configuration.toml" "/etc/kata-containers/configuration.toml"
cp "deploy/kata-configuration-cvm.toml" "/etc/kata-containers/configuration-cvm.toml"

# Конфигурация Docker
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
echo -e "${GREEN}✓ Kata Containers configured${NC}"

# --- Шаг 6: Сборка и Установка Агента ---
echo -e "${YELLOW}[6/7] Building QuData Agent...${NC}"

go mod tidy

CGO_ENABLED=1 go build -ldflags="-s -w" -o /usr/local/bin/qudata-agent ./cmd/agent
chmod +x /usr/local/bin/qudata-agent
mkdir -p "$INSTALL_DIR"
echo -e "${GREEN}✓ Agent binary compiled and installed${NC}"

# --- Шаг 7: Запуск сервиса ---
echo -e "${YELLOW}[7/7] Starting Services...${NC}"

# Auditd Rules
tee "/etc/audit/rules.d/99-qudata.rules" > /dev/null <<EOF
-w /usr/bin/virsh -p x -k qudata_exec_watch
-w /usr/bin/qemu-img -p x -k qudata_exec_watch
EOF
augenrules --load 2>/dev/null || true
systemctl restart auditd

# AppArmor
PROFILE_PATH="/etc/apparmor.d/usr.local.bin.qudata-agent"
cp "deploy/qudata-agent.profile" "$PROFILE_PATH"
apparmor_parser -r "$PROFILE_PATH"
aa-complain "qudata-agent" 2>/dev/null || true

# Systemd Service
cp "deploy/qudata-agent.service" /etc/systemd/system/qudata-agent.service
sed -i "s/YOUR_API_KEY_PLACEHOLDER/$API_KEY/g" /etc/systemd/system/qudata-agent.service
systemctl daemon-reload
systemctl enable --now qudata-agent.service

echo "  Waiting for agent to start..."
sleep 5

# Активация Authz плагина (если агент успешно поднял сокет)
if [ -S "/run/docker/plugins/qudata-authz.sock" ]; then
    echo "  Activating Docker authorization plugin..."
    mkdir -p /etc/docker/plugins
    echo '{"Socket": "qudata-authz.sock"}' > /etc/docker/plugins/qudata-authz.json

    TEMP_JSON=$(mktemp)
    jq '.["authorization-plugins"] = ["qudata-authz"]' /etc/docker/daemon.json > "$TEMP_JSON" && mv "$TEMP_JSON" /etc/docker/daemon.json
    systemctl restart docker
    echo -e "${GREEN}✓ Authz plugin activated${NC}"
else
    echo -e "${RED}Warning: Authz socket not found. Agent might have failed to start.${NC}"
fi

echo ""
echo -e "${GREEN}>>> INSTALLATION COMPLETE! <<<${NC}"
echo "Check status: sudo systemctl status qudata-agent"