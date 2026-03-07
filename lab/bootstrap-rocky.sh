#!/usr/bin/env bash
# bootstrap-rocky.sh — Bootstrap a fresh Rocky Linux 10 (or RHEL-compatible) machine
# for the netbird-events lab environment.
#
# Usage:
#   sudo bash bootstrap-rocky.sh
#   sudo bash bootstrap-rocky.sh --user david --ssh-key "ssh-ed25519 AAAA..."
#   ssh root@192.168.101.6 'bash -s' < bootstrap-rocky.sh -- --user david --ssh-key "$(cat ~/.ssh/id_ed25519.pub)"
set -euo pipefail

# ============================================================================
# Colors
# ============================================================================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${GREEN}[bootstrap]${NC} $*"; }
warn() { echo -e "${YELLOW}[bootstrap]${NC} $*"; }
err()  { echo -e "${RED}[bootstrap]${NC} $*" >&2; }
info() { echo -e "${CYAN}[bootstrap]${NC} $*"; }

# ============================================================================
# Must run as root
# ============================================================================
if [[ $EUID -ne 0 ]]; then
  err "This script must be run as root. Try: sudo bash $0 $*"
  exit 1
fi

# ============================================================================
# Parse arguments
# ============================================================================
TARGET_USER=""
SSH_KEY=""
READ_KEY_FROM_STDIN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --user)
      TARGET_USER="$2"
      shift 2
      ;;
    --user=*)
      TARGET_USER="${1#--user=}"
      shift
      ;;
    --ssh-key)
      if [[ "${2:-}" == "-" ]]; then
        READ_KEY_FROM_STDIN=true
        shift 2
      else
        SSH_KEY="$2"
        shift 2
      fi
      ;;
    --ssh-key=*)
      val="${1#--ssh-key=}"
      if [[ "$val" == "-" ]]; then
        READ_KEY_FROM_STDIN=true
      else
        SSH_KEY="$val"
      fi
      shift
      ;;
    --)
      shift
      ;;
    *)
      warn "Unknown argument: $1 (ignoring)"
      shift
      ;;
  esac
done

# Determine target user
if [[ -z "$TARGET_USER" ]]; then
  # Try SUDO_USER (set when running via sudo)
  if [[ -n "${SUDO_USER:-}" && "$SUDO_USER" != "root" ]]; then
    TARGET_USER="$SUDO_USER"
    info "Auto-detected user from SUDO_USER: $TARGET_USER"
  else
    # Prompt — only if running interactively
    if [[ -t 0 ]]; then
      read -rp "Enter username to set up: " TARGET_USER
      if [[ -z "$TARGET_USER" ]]; then
        err "No username provided. Use --user <name>"
        exit 1
      fi
    else
      err "Cannot determine target user. Pass --user <name>"
      exit 1
    fi
  fi
fi

# Read SSH key from stdin if requested
if [[ "$READ_KEY_FROM_STDIN" == true ]]; then
  log "Reading SSH public key from stdin..."
  SSH_KEY="$(cat)"
fi

info "Target user: $TARGET_USER"
if [[ -n "$SSH_KEY" ]]; then
  info "SSH key: ${SSH_KEY:0:40}..."
else
  info "SSH key: not provided (skipping)"
fi

# ============================================================================
# Detect OS
# ============================================================================
if [[ -f /etc/os-release ]]; then
  # shellcheck disable=SC1091
  source /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION_ID="${VERSION_ID:-0}"
  OS_NAME="${PRETTY_NAME:-unknown}"
else
  OS_ID="unknown"
  OS_VERSION_ID="0"
  OS_NAME="unknown"
fi

log "Detected OS: $OS_NAME"

case "$OS_ID" in
  rhel|rocky|almalinux|centos|ol|fedora) : ;;
  *)
    warn "This script targets RHEL-compatible distros. Detected: $OS_ID"
    warn "Continuing anyway — some steps may fail."
    ;;
esac

# ============================================================================
# Track completed steps (idempotency)
# ============================================================================
BOOTSTRAP_STATE_DIR="/var/lib/bootstrap-rocky"
mkdir -p "$BOOTSTRAP_STATE_DIR"

step_done() { [[ -f "$BOOTSTRAP_STATE_DIR/$1" ]]; }
mark_done() { touch "$BOOTSTRAP_STATE_DIR/$1"; }

# Summary tracking
SUMMARY=()
add_summary() { SUMMARY+=("$1"); }

# ============================================================================
# Step 1: System update + base packages
# ============================================================================
log "==> Step 1: Installing base packages..."

if ! step_done "base_packages"; then
  dnf -y update --quiet
  dnf -y install --quiet \
    git \
    curl \
    wget \
    jq \
    ca-certificates \
    gnupg \
    sudo \
    bash \
    tar \
    gzip
  mark_done "base_packages"
  add_summary "Installed base packages (git, curl, jq, etc.)"
else
  log "Base packages already installed — skipping"
fi

# ============================================================================
# Step 2: Install Docker CE
# ============================================================================
log "==> Step 2: Installing Docker Engine..."

if ! step_done "docker_installed" && ! command -v docker &>/dev/null; then
  # Add Docker CE repo for RHEL-compatible
  dnf -y install --quiet dnf-plugins-core
  dnf config-manager --add-repo https://download.docker.com/linux/rhel/docker-ce.repo

  dnf -y install --quiet \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

  mark_done "docker_installed"
  add_summary "Installed Docker CE + Compose plugin + buildx"
elif command -v docker &>/dev/null; then
  log "Docker already installed ($(docker --version)) — skipping"
  mark_done "docker_installed"
else
  log "Docker already installed — skipping"
fi

# ============================================================================
# Step 3: Install yq
# ============================================================================
log "==> Step 3: Installing yq..."

if ! step_done "yq_installed" && ! command -v yq &>/dev/null; then
  YQ_BIN="/usr/local/bin/yq"
  log "Downloading yq from GitHub releases..."
  curl -fsSL "https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64" -o "$YQ_BIN"
  chmod +x "$YQ_BIN"
  mark_done "yq_installed"
  add_summary "Installed yq $(yq --version 2>/dev/null | head -1)"
elif command -v yq &>/dev/null; then
  log "yq already installed ($(yq --version 2>/dev/null | head -1)) — skipping"
  mark_done "yq_installed"
else
  log "yq already installed — skipping"
fi

# ============================================================================
# Step 4: Create user
# ============================================================================
log "==> Step 4: Setting up user '$TARGET_USER'..."

if ! id "$TARGET_USER" &>/dev/null; then
  useradd -m -s /bin/bash "$TARGET_USER"
  log "Created user: $TARGET_USER"
  add_summary "Created user: $TARGET_USER"
else
  log "User $TARGET_USER already exists — skipping creation"
fi

USER_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
log "User home: $USER_HOME"

# ============================================================================
# Step 5: SSH key
# ============================================================================
log "==> Step 5: Configuring SSH access..."

if [[ -n "$SSH_KEY" ]]; then
  SSH_DIR="$USER_HOME/.ssh"
  AUTH_KEYS="$SSH_DIR/authorized_keys"

  mkdir -p "$SSH_DIR"
  chmod 700 "$SSH_DIR"

  # Add key only if not already present
  if ! grep -qF "$SSH_KEY" "$AUTH_KEYS" 2>/dev/null; then
    echo "$SSH_KEY" >> "$AUTH_KEYS"
    log "Added SSH public key to $AUTH_KEYS"
    add_summary "Added SSH public key for $TARGET_USER"
  else
    log "SSH key already in authorized_keys — skipping"
  fi

  chmod 600 "$AUTH_KEYS"
  chown -R "$TARGET_USER:$TARGET_USER" "$SSH_DIR"
else
  warn "No SSH key provided — skipping authorized_keys setup"
fi

# ============================================================================
# Step 6: Passwordless sudo
# ============================================================================
log "==> Step 6: Configuring passwordless sudo..."

SUDOERS_FILE="/etc/sudoers.d/${TARGET_USER}"

if [[ ! -f "$SUDOERS_FILE" ]]; then
  echo "${TARGET_USER} ALL=(ALL) NOPASSWD:ALL" > "$SUDOERS_FILE"
  chmod 440 "$SUDOERS_FILE"
  log "Created $SUDOERS_FILE"
  add_summary "Configured passwordless sudo for $TARGET_USER"
else
  log "Sudoers file already exists: $SUDOERS_FILE — skipping"
fi

# Validate sudoers file
if visudo -c -f "$SUDOERS_FILE" &>/dev/null; then
  log "Sudoers file is valid"
else
  err "Sudoers file validation failed! Removing to be safe."
  rm -f "$SUDOERS_FILE"
  exit 1
fi

# ============================================================================
# Step 7: Docker group membership
# ============================================================================
log "==> Step 7: Adding $TARGET_USER to docker group..."

if ! groups "$TARGET_USER" | grep -q '\bdocker\b'; then
  usermod -aG docker "$TARGET_USER"
  log "Added $TARGET_USER to docker group"
  add_summary "Added $TARGET_USER to docker group (re-login required for effect)"
else
  log "$TARGET_USER is already in docker group — skipping"
fi

# ============================================================================
# Step 8: Enable and start Docker
# ============================================================================
log "==> Step 8: Enabling Docker service..."

if ! step_done "docker_enabled"; then
  systemctl enable --now docker
  mark_done "docker_enabled"
  add_summary "Enabled and started Docker service"
else
  # Ensure it's running even if previously enabled
  if ! systemctl is-active --quiet docker; then
    systemctl start docker
    log "Started Docker service"
  else
    log "Docker service already running — skipping"
  fi
fi

# Verify Docker is working
if docker info &>/dev/null; then
  log "Docker is running OK"
else
  warn "Docker may not be fully started yet — check with: systemctl status docker"
fi

# ============================================================================
# Step 9: Clone repos
# ============================================================================
log "==> Step 9: Cloning repositories..."

clone_or_pull() {
  local repo_url="$1"
  local dest="$2"
  local name
  name="$(basename "$dest")"

  if [[ -d "$dest/.git" ]]; then
    log "$name already cloned — pulling latest..."
    sudo -u "$TARGET_USER" git -C "$dest" pull --ff-only 2>/dev/null || \
      warn "git pull failed for $name (maybe on a detached HEAD or dirty — skipping)"
  else
    log "Cloning $name..."
    sudo -u "$TARGET_USER" git clone "$repo_url" "$dest"
    add_summary "Cloned $name → $dest"
  fi
}

clone_or_pull \
  "https://github.com/netbirdio/netbird.git" \
  "$USER_HOME/netbird"

clone_or_pull \
  "https://github.com/xh63/netbird-events.git" \
  "$USER_HOME/netbird-events"

# Ensure correct ownership (git clone as sudo -u should handle this, but be safe)
chown -R "$TARGET_USER:$TARGET_USER" "$USER_HOME/netbird" 2>/dev/null || true
chown -R "$TARGET_USER:$TARGET_USER" "$USER_HOME/netbird-events" 2>/dev/null || true

# ============================================================================
# Step 10: Create secrets directory
# ============================================================================
log "==> Step 10: Setting up secrets directory..."

SECRETS_DIR="$USER_HOME/netbird-events/lab/secrets"
if [[ -d "$SECRETS_DIR" ]]; then
  log "Secrets directory already exists: $SECRETS_DIR"
else
  mkdir -p "$SECRETS_DIR"
  chown "$TARGET_USER:$TARGET_USER" "$SECRETS_DIR"
  log "Created secrets directory: $SECRETS_DIR"
fi

# ============================================================================
# Print summary
# ============================================================================
echo ""
echo -e "${GREEN}============================================================${NC}"
echo -e "${GREEN}  Bootstrap complete!${NC}"
echo -e "${GREEN}============================================================${NC}"
echo ""

if [[ ${#SUMMARY[@]} -gt 0 ]]; then
  echo -e "${CYAN}What was done:${NC}"
  for item in "${SUMMARY[@]}"; do
    echo "  - $item"
  done
  echo ""
fi

echo -e "${CYAN}Next steps:${NC}"
echo ""
echo "  1. Log in as $TARGET_USER:"
if [[ -n "${SSH_KEY:-}" ]]; then
  echo "       ssh ${TARGET_USER}@$(hostname -I | awk '{print $1}')"
else
  echo "       ssh ${TARGET_USER}@$(hostname -I | awk '{print $1}')"
  warn "     No SSH key was configured — set a password first:"
  echo "       passwd $TARGET_USER"
fi
echo ""
echo "  2. NOTE: Docker group membership requires a new login to take effect."
echo "     The first docker command after login will work without sudo."
echo ""
echo "  3. Place your Cloudflare API token (for real NetBird mode):"
echo "       ~/netbird-events/lab/secrets/cf-token"
echo ""
echo "  4. Configure and run the lab:"
echo "       cd ~/netbird-events/lab"
echo "       cp lab.env.example lab.env"
echo "       vi lab.env"
echo "       ./lab-setup.sh"
echo ""
echo -e "${GREEN}============================================================${NC}"
