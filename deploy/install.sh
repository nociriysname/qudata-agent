#!/bin/bash

set -e

# --- Цвета и шапка ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo ""
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║        QuData Agent Installer          ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
echo ""

# --- Проверки ---
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root${NC}"; exit 1;
fi
if [ -z "$1" ]; then
    echo -e "${RED}Error: API key is required${NC}"; exit 1;
fi

API_KEY="$1"
INSTALL_DIR="/opt/qudata-agent"
KATA_VERSION="3.2.0"

# --- Шаг 1: Системные зависимости ---
echo -e "${YELLOW}[1/7] Installing system dependencies...${NC}"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y --no-install-recommends \
    curl wget gnupg lsb-release build-essential git \
    cryptsetup auditd apparmor-utils \
    jq tar xz-utils ubuntu-drivers-common 2>&1 | grep -v "^Reading\|^Building" || true
echo -e "${GREEN}✓ System dependencies installed${NC}"

# --- Шаг 2: Docker ---
echo -e "${YELLOW}[2/7] Installing Docker...${NC}"
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    sh /tmp/get-docker.sh > /dev/null 2>&1
fi
systemctl enable --now docker > /dev/null 2>&1
echo -e "${GREEN}✓ Docker is running${NC}"

# --- Шаг 3: Установка и проверка NVIDIA ---
echo -e "${YELLOW}[3/7] Installing and verifying NVIDIA components...${NC}"
if ! lspci | grep -iq nvidia; then
    echo -e "${YELLOW}Warning: No NVIDIA GPU detected. Skipping NVIDIA setup.${NC}"
else
    echo "  NVIDIA GPU detected."
    if command -v nvidia-smi &> /dev/null && nvidia-smi &> /dev/null; then
        echo "  NVIDIA driver is already installed and working."
    else
        echo "  NVIDIA driver not found or not working. Attempting installation..."
        ubuntu-drivers autoinstall
        echo -e "${YELLOW}NVIDIA driver installation initiated. A REBOOT IS REQUIRED to apply changes.${NC}"
        echo -e "${YELLOW}After reboot, please run this install script again.${NC}"
        exit 0
    fi

    echo "  Installing NVIDIA Container Toolkit..."
    curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
      && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
        sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
        tee /etc/apt/sources.list.d/nvidia-container-toolkit.list > /dev/null
    apt-get update -qq
    apt-get install -y --no-install-recommends nvidia-container-toolkit 2>&1 | grep -v "^Reading\|^Building" || true

    nvidia-ctk runtime configure --runtime=docker
    systemctl restart docker
    echo -e "${GREEN}✓ NVIDIA components installed${NC}"
fi

# --- Шаг 4: Kata Containers ---
echo -e "${YELLOW}[4/7] Installing and configuring Kata Containers v${KATA_VERSION}...${NC}"
if ! command -v kata-runtime &> /dev/null; then
    ARCH=$(uname -m)
    if [ "$ARCH" != "x86_64" ]; then
        echo -e "${RED}Unsupported architecture: $ARCH${NC}"; exit 1;
    fi
    KATA_PACKAGE="kata-static-${KATA_VERSION}-amd64.tar.xz"
    KATA_URL="https://github.com/kata-containers/kata-containers/releases/download/${KATA_VERSION}/${KATA_PACKAGE}"
    echo "  Downloading Kata package..."
    curl -fLo "/tmp/$KATA_PACKAGE" "$KATA_URL"
    echo "  Creating /opt/kata directory..."
    mkdir -p /opt/kata
    echo "  Extracting package to /opt/kata..."
    tar -xJf "/tmp/$KATA_PACKAGE" -C /opt/kata --strip-components=1
    rm -f "/tmp/$KATA_PACKAGE"
    echo "  Creating symlink for kata-runtime..."
    ln -sf /opt/kata/bin/kata-runtime /usr/local/bin/kata-runtime
fi
echo "  Creating Kata configuration files..."
mkdir -p /etc/kata-containers
cp "deploy/kata-configuration.toml" "/etc/kata-containers/configuration.toml"
cp "deploy/kata-configuration-cvm.toml" "/etc/kata-containers/configuration-cvm.toml"

# --- Шаг 5: Конфигурация Docker ---
echo -e "${YELLOW}[5/7] Configuring Docker daemon for Kata...${NC}"
mkdir -p /etc/docker
if [ ! -f /etc/docker/daemon.json ]; then echo '{}' > /etc/docker/daemon.json; fi
TEMP_JSON=$(mktemp)
jq '.runtimes = (.runtimes // {}) + {
    "kata-qemu": {"path": "/usr/local/bin/kata-runtime", "runtimeArgs": ["--kata-config-path=/etc/kata-containers/configuration.toml"]},
    "kata-cvm": {"path": "/usr/local/bin/kata-runtime", "runtimeArgs": ["--kata-config-path=/etc/kata-containers/configuration-cvm.toml"]}
}' /etc/docker/daemon.json > "$TEMP_JSON" && mv "$TEMP_JSON" /etc/docker/daemon.json
systemctl restart docker
echo -e "${GREEN}✓ Docker daemon configured for Kata${NC}"

# --- Шаг 6: Установка Go и Сборка Агента ---
echo -e "${YELLOW}[6/7] Building QuData Agent...${NC}"
REQUIRED_GO_VERSION=$(grep -oP '^go\s+\K[0-9]+\.[0-9]+(\.[0-9]+)?' go.mod)
if [ -z "$REQUIRED_GO_VERSION" ]; then
    echo -e "${RED}Error: Could not determine Go version from go.mod. Using default.${NC}"
    REQUIRED_GO_VERSION="1.25.0"
fi
INSTALLED_GO_VERSION=""
if [ -x "/usr/local/go/bin/go" ]; then
    INSTALLED_GO_VERSION=$("/usr/local/go/bin/go" version 2>/dev/null | grep -oP 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | sed 's/go//')
fi
if [ "$INSTALLED_GO_VERSION" != "$REQUIRED_GO_VERSION" ]; then
    echo "  Go v${REQUIRED_GO_VERSION} not found or version mismatch (found v${INSTALLED_GO_VERSION:-none}). Installing..."
    GO_TARBALL="go${REQUIRED_GO_VERSION}.linux-amd64.tar.gz"
    wget -q "https://go.dev/dl/${GO_TARBALL}" -O "/tmp/${GO_TARBALL}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TARBALL}"
    rm "/tmp/${GO_TARBALL}"
fi
echo "  Building agent binary (CGO disabled)..."
CGO_ENABLED=0 /usr/local/go/bin/go build -ldflags="-s -w" -o /usr/local/bin/qudata-agent ./cmd/agent
chmod +x /usr/local/bin/qudata-agent
mkdir -p "$INSTALL_DIR"
echo -e "${GREEN}✓ Agent binary installed${NC}"

# --- Шаг 7: Запуск сервиса ---
echo -e "${YELLOW}[7/7] Starting QuData Agent service...${NC}"
tee "/etc/audit/rules.d/99-qudata.rules" > /dev/null <<EOF
-w /usr/bin/virsh -p x -k qudata_exec_watch
-w /usr/bin/qemu-img -p x -k qudata_exec_watch
EOF
augenrules --load || systemctl restart auditd
PROFILE_PATH="/etc/apparmor.d/usr.local.bin.qudata-agent"
cp "deploy/qudata-agent.profile" "$PROFILE_PATH"
apparmor_parser -r "$PROFILE_PATH"
aa-enforce "qudata-agent" 2>/dev/null || true

# Запуск сервиса
cp "deploy/qudata-agent.service" /etc/systemd/system/qudata-agent.service
sed -i "s/YOUR_API_KEY_PLACEHOLDER/$API_KEY/g" /etc/systemd/system/qudata-agent.service
systemctl daemon-reload
systemctl enable --now qudata-agent.service

echo "  Waiting for agent to start and create authz socket..."
sleep 5
if ! systemctl is-active --quiet qudata-agent.service; then
    echo -e "${RED}Error: Agent service failed to start. Please check logs:${NC}"
    echo "  sudo journalctl -u qudata-agent -n 50"
    exit 1
fi

# Активация Authz плагина
echo "  Activating Docker authorization plugin..."
mkdir -p /etc/docker/plugins
echo '{"Socket": "qudata-authz.sock"}' > /etc/docker/plugins/qudata-authz.json
TEMP_JSON=$(mktemp)
jq '.["authorization-plugins"] = ["qudata-authz"]' /etc/docker/daemon.json > "$TEMP_JSON" && mv "$TEMP_JSON" /etc/docker/daemon.json
systemctl restart docker
echo -e "${GREEN}✓ Agent is running and Authz plugin is active!${NC}"

echo ""
echo -e "${GREEN}Installation Completed!${NC}"
echo "  ${GREEN}View logs:${NC}      sudo journalctl -u qudata-agent -f"
echo "  ${GREEN}Status:${NC}         sudo systemctl status qudata-agent"