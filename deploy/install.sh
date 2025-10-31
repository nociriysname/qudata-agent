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
echo -e "${BLUE}║     QuData Agent (Go) Installer        ║${NC}"
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
echo -e "${YELLOW}[1/6] Installing system dependencies...${NC}"
apt-get update -qq
apt-get install -y --no-install-recommends \
    curl wget gnupg lsb-release \
    cryptsetup auditd apparmor-utils \
    jq tar xz-utils 2>&1 | grep -v "^Reading\|^Building" || true
echo -e "${GREEN}✓ System dependencies installed${NC}"

# --- Шаг 2: Docker ---
echo -e "${YELLOW}[2/6] Installing Docker...${NC}"
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    sh /tmp/get-docker.sh > /dev/null 2>&1
fi
systemctl enable --now docker > /dev/null 2>&1
echo -e "${GREEN}✓ Docker is running${NC}"

# --- Шаг 3: Kata Containers ---
echo -e "${YELLOW}[3/6] Installing Kata Containers v${KATA_VERSION}...${NC}"
if ! command -v kata-runtime &> /dev/null; then
    ARCH=$(uname -m)
    [ "$ARCH" != "x86_64" ] && { echo -e "${RED}Unsupported architecture: $ARCH${NC}"; exit 1; }

    KATA_PACKAGE="kata-static-${KATA_VERSION}-amd64.tar.xz"
    KATA_URL="https://github.com/kata-containers/kata-containers/releases/download/${KATA_VERSION}/${KATA_PACKAGE}"

    echo "  Downloading Kata package..."
    curl -fLo "/tmp/$KATA_PACKAGE" "$KATA_URL"

    echo "  Extracting package to /opt/kata..."
    mkdir -p /opt/kata
    tar -xJf "/tmp/$KATA_PACKAGE" -C /opt/kata
    rm -f "/tmp/$KATA_PACKAGE"

    echo "  Creating symlink for kata-runtime..."
    ln -sf /opt/kata/bin/kata-runtime /usr/local/bin/kata-runtime

    echo "  Configuring Docker for Kata..."
    mkdir -p /etc/docker
    cat /etc/docker/daemon.json | jq '. + {"runtimes": {"kata-qemu": {"path": "/usr/local/bin/kata-runtime"}}}' > /etc/docker/daemon.json.tmp && mv /etc/docker/daemon.json.tmp /etc/docker/daemon.json

    systemctl restart docker
    echo -e "${GREEN}✓ Kata Containers installed and configured${NC}"
else
    echo -e "${GREEN}✓ Kata Containers already installed${NC}"
fi

# --- Шаг 4: Сборка и установка Go-агента ---
echo -e "${YELLOW}[4/6] Building and installing QuData Agent...${NC}"
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: go.mod not found. Please run this script from the project root directory.${NC}"; exit 1;
fi

echo "  Building agent binary..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/qudata-agent ./cmd/agent
chmod +x /usr/local/bin/qudata-agent

mkdir -p "$INSTALL_DIR"
echo -e "${GREEN}✓ Agent binary installed to /usr/local/bin/qudata-agent${NC}"

# --- Шаг 5: Настройка безопасности ---
echo -e "${YELLOW}[5/6] Configuring security modules (Auditd, AppArmor)...${NC}"
tee "/etc/audit/rules.d/99-qudata.rules" > /dev/null <<EOF
-w /usr/bin/virsh -p x -k qudata_exec_watch
-w /usr/bin/qemu-img -p x -k qudata_exec_watch
EOF
augenrules --load || systemctl restart auditd

# AppArmor
PROFILE_PATH="/etc/apparmor.d/usr.local.bin.qudata-agent"
cp "deploy/qudata-agent.profile" "$PROFILE_PATH"
apparmor_parser -r "$PROFILE_PATH"
aa-enforce "qudata-agent" 2>/dev/null || true
echo -e "${GREEN}✓ Security modules configured${NC}"

# --- Шаг 6: Создание и запуск сервиса ---
echo -e "${YELLOW}[6/6] Starting QuData Agent service...${NC}"
cp "deploy/qudata-agent.service" /etc/systemd/system/qudata-agent.service
sed -i "s/YOUR_API_KEY_PLACEHOLDER/$API_KEY/g" /etc/systemd/system/qudata-agent.service

systemctl daemon-reload
systemctl enable --now qudata-agent.service

echo "  Waiting for agent to start..."
sleep 3
if ! systemctl is-active --quiet qudata-agent.service; then
    echo -e "${RED}Error: Agent service failed to start. Please check logs:${NC}"
    echo "  sudo journalctl -u qudata-agent -n 50"
    exit 1
fi
echo -e "${GREEN}✓ Agent is running successfully!${NC}"

echo ""
echo -e "${GREEN}Installation Completed!${NC}"
echo "  ${GREEN}View logs:${NC}      sudo journalctl -u qudata-agent -f"
echo "  ${GREEN}Status:${NC}         sudo systemctl status qudata-agent"