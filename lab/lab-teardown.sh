#!/usr/bin/env bash
# lab-teardown.sh — Destroys the entire lab environment
# Usage: ./lab-teardown.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[teardown]${NC} $*"; }
warn() { echo -e "${YELLOW}[teardown]${NC} $*"; }

echo -e "${RED}This will destroy ALL lab data (databases, logs, configs).${NC}"
read -rp "Are you sure? [y/N] " confirm
if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
  echo "Aborted."
  exit 0
fi

# Stop and remove lab services
log "Stopping lab services..."
docker compose -f "$SCRIPT_DIR/docker-compose.lab.yml" down --volumes --rmi local 2>/dev/null || true

# Stop and remove NetBird services
# Preserve the Traefik letsencrypt volume (netbird_netbird_traefik_letsencrypt) so
# Let's Encrypt certs survive teardown/rebuild cycles. Requesting a new cert on
# every rebuild exhausts the LE rate limit (5 certs per exact domain set per week).
# Only remove the data volume (netbird_netbird_data); the cert volume is kept.
NETBIRD_DIR="$SCRIPT_DIR/data/netbird"
if [[ -f "$NETBIRD_DIR/docker-compose.yml" ]]; then
  log "Stopping NetBird services (preserving TLS cert volume)..."
  cd "$NETBIRD_DIR"
  docker compose down 2>/dev/null || true
  docker volume rm netbird_netbird_data 2>/dev/null || true
  cd "$SCRIPT_DIR"
fi

# Remove shared network
log "Removing lab-net network..."
docker network rm lab-net 2>/dev/null || true

# Remove all data (sudo needed because containers run as root)
log "Removing data directory..."
sudo rm -rf "$SCRIPT_DIR/data/"

log "Lab environment destroyed"
echo ""
echo -e "To recreate: ${GREEN}./lab-setup.sh${NC}"
