#!/usr/bin/env bash
# lab-setup.sh — Sets up complete netbird-events lab environment
# Usage: ./lab-setup.sh
#
# Requires: docker, docker compose, jq
# Optional: yq (for auto-patching NetBird compose), NetBird repo clone
#           sqlite3 (only required when DB_MODE=2 / SQLite mode)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${GREEN}[lab]${NC} $*"; }
warn() { echo -e "${YELLOW}[lab]${NC} $*"; }
err()  { echo -e "${RED}[lab]${NC} $*" >&2; }
info() { echo -e "${CYAN}[lab]${NC} $*"; }

# ============================================================================
# 1. Check prerequisites (auto-install on Rocky Linux if missing)
# ============================================================================
log "Checking prerequisites..."

IS_ROCKY=false
if grep -qi "rocky" /etc/os-release 2>/dev/null; then
  IS_ROCKY=true
fi

# Auto-install a package via dnf (Rocky Linux only)
dnf_install() {
  local pkg=$1
  log "Installing $pkg via dnf..."
  sudo dnf install -y "$pkg" &>/dev/null \
    && log "$pkg installed" \
    || { err "Failed to install $pkg — install it manually and re-run"; exit 1; }
}

# docker — if missing on Rocky, install Docker CE from the official repo
if ! command -v docker &>/dev/null; then
  if $IS_ROCKY; then
    log "docker not found — installing Docker CE..."
    sudo dnf config-manager --add-repo https://download.docker.com/linux/rhel/docker-ce.repo &>/dev/null
    sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin &>/dev/null \
      && sudo systemctl enable --now docker \
      && sudo usermod -aG docker "$USER" \
      && log "Docker CE installed — you may need to log out and back in for group membership to take effect" \
      || { err "Failed to install Docker CE"; exit 1; }
  else
    err "Required command not found: docker"
    exit 1
  fi
fi

# docker compose plugin
if ! docker compose version &>/dev/null; then
  if $IS_ROCKY; then
    dnf_install docker-compose-plugin
  else
    err "docker compose plugin not found"
    exit 1
  fi
fi

# jq
if ! command -v jq &>/dev/null; then
  if $IS_ROCKY; then
    dnf_install jq
  else
    err "Required command not found: jq"
    exit 1
  fi
fi

# yq (snap only — not in dnf)
if ! command -v yq &>/dev/null && $IS_ROCKY; then
  log "yq not found — installing via snap..."
  if ! command -v snap &>/dev/null; then
    sudo dnf install -y snapd &>/dev/null && sudo systemctl enable --now snapd.socket &>/dev/null || true
    sudo ln -sf /var/lib/snapd/snap /snap 2>/dev/null || true
    sleep 5  # snapd needs a moment after first install
  fi
  sudo snap install yq &>/dev/null && log "yq installed" \
    || warn "yq install failed — NetBird compose patching (LAB_MODE=2) will not work"
fi

log "Prerequisites OK"

# ============================================================================
# 2. Source lab.env (copy from lab.env.example on first run)
# ============================================================================
if [[ ! -f "$SCRIPT_DIR/lab.env" ]]; then
  if [[ -f "$SCRIPT_DIR/lab.env.example" ]]; then
    cp "$SCRIPT_DIR/lab.env.example" "$SCRIPT_DIR/lab.env"
    log "Created lab.env from lab.env.example — edit it to customise"
  else
    warn "lab.env not found, using defaults"
  fi
fi

if [[ -f "$SCRIPT_DIR/lab.env" ]]; then
  log "Loading lab.env..."
  set -a
  # shellcheck source=lab.env
  source "$SCRIPT_DIR/lab.env"
  set +a
fi

# Defaults
: "${NETBIRD_DOMAIN:=use-ip}"
: "${REVERSE_PROXY_TYPE:=0}"
: "${TRAEFIK_ACME_EMAIL:=admin@example.com}"
: "${ENABLE_PROXY:=false}"
: "${POSTGRES_USER:=netbird}"
: "${POSTGRES_PASSWORD:=netbird-lab-pass}"
: "${POSTGRES_DB:=netbird}"
: "${POSTGRES_PORT:=5432}"
: "${EP_PLATFORM:=sandbox}"
: "${EP_REGION:=apac}"
: "${EP_LOG_LEVEL:=debug}"
: "${EP_BATCH_SIZE:=100}"
: "${EP_POLLING_INTERVAL:=30}"
: "${EP_METRICS_PORT:=2113}"
: "${EP_EMAIL_ENRICHMENT_SOURCE:=netbird_users}"
# Path to NetBird's config.yaml inside the container — used to read the
# store.encryptionKey for AES-GCM decryption of email/name fields.
# Set automatically for real NetBird mode (LAB_MODE=2); leave empty for
# simulated mode (stub data is not encrypted).
: "${EP_NETBIRD_CONFIG_PATH:=}"
: "${EP_DATABASE_DRIVER:=}"
: "${GRAFANA_PORT:=3000}"
: "${GRAFANA_ADMIN_PASSWORD:=admin}"
: "${LOKI_PORT:=3100}"
: "${SPLUNK_PORT:=8000}"
: "${SPLUNK_HEC_PORT:=8088}"
: "${SPLUNK_HEC_TOKEN:=lab-hec-token-123}"
: "${SPLUNK_PASSWORD:=changeme123}"
: "${NETBIRD_REPO:=}"

# ============================================================================
# 3. Prompt: simulated or real NetBird?
# ============================================================================
echo ""
echo -e "${BLUE}============================================================================${NC}"
echo -e "${BLUE}  Lab Mode Selection${NC}"
echo -e "${BLUE}============================================================================${NC}"
echo ""
echo -e "  ${CYAN}[1]${NC} Simulated  — stub NetBird tables with realistic test data"
echo -e "       No NetBird instance needed. eventsproc processes fake events."
echo ""
echo -e "  ${CYAN}[2]${NC} Real NetBird — full stack with NetBird + Traefik + Cloudflare TLS"
echo -e "       Requires: NetBird repo clone + secrets/cf-token"
echo ""
if [[ -n "${LAB_MODE:-}" ]]; then
  echo -e "  Using LAB_MODE=${LAB_MODE} from environment"
else
  echo -n "  Choose [1/2] (default: 1): "
  read -r LAB_MODE_INPUT < /dev/tty
  LAB_MODE="${LAB_MODE_INPUT:-1}"
fi

if [[ "$LAB_MODE" != "1" && "$LAB_MODE" != "2" ]]; then
  err "Invalid choice '$LAB_MODE'. Must be 1 or 2."
  exit 1
fi

if [[ "$LAB_MODE" == "2" ]]; then
  # Real NetBird: auto-set the decryption key path (container mount defined in docker-compose)
  : "${EP_NETBIRD_CONFIG_PATH:=/etc/netbird/config.yaml}"
  if [[ "$DB_MODE" == "2" ]]; then
    # SQLite mode: activity events are in events.db; users table is in store.db (different file).
    # Cross-database JOINs aren't supported without ATTACH — disable email enrichment.
    EP_EMAIL_ENRICHMENT_SOURCE="none"
  fi
  # Validate NETBIRD_DOMAIN is set
  if [[ -z "$NETBIRD_DOMAIN" || "$NETBIRD_DOMAIN" == "use-ip" ]]; then
    err "NETBIRD_DOMAIN must be set to the DNS name pointing at this server (e.g. netbird.example.com)"
    err "Edit lab.env and set NETBIRD_DOMAIN before running in real NetBird mode"
    exit 1
  fi
  # Validate cf-token exists before proceeding
  CF_TOKEN_FILE="$SCRIPT_DIR/secrets/cf-token"
  if [[ ! -f "$CF_TOKEN_FILE" ]]; then
    err "Real NetBird mode requires secrets/cf-token (Cloudflare API token)"
    err "Copy it with: scp your-mac:/path/to/cf-token openclaw:~/netbird-events/lab/secrets/cf-token"
    exit 1
  fi
  log "Mode: Real NetBird (Cloudflare DNS challenge) — domain: ${NETBIRD_DOMAIN}"
else
  log "Mode: Simulated (stub data)"
fi
echo ""

# ============================================================================
# 3b. Database mode selection
# ============================================================================
echo -e "${BLUE}============================================================================${NC}"
echo -e "${BLUE}  Database Mode${NC}"
echo -e "${BLUE}============================================================================${NC}"
echo ""
echo -e "  ${CYAN}[1]${NC} PostgreSQL (default) — managed Postgres shared by NetBird + eventsproc"
echo -e "       Full-featured; required for cluster/HA mode"
echo ""
echo -e "  ${CYAN}[2]${NC} SQLite — NetBird's built-in file-based store (EP_DATABASE_DRIVER=sqlite)"
echo -e "       Simulated: creates a local store.db with test events"
echo -e "       Real NetBird: reads directly from NetBird's own store.db"
echo -e "       Note: cluster/HA mode is not supported with SQLite"
echo ""

# Allow pre-seeding DB_MODE from EP_DATABASE_DRIVER env var
if [[ "${EP_DATABASE_DRIVER}" == "sqlite" ]]; then
  DB_MODE="2"
elif [[ "${EP_DATABASE_DRIVER}" == "postgres" ]]; then
  DB_MODE="1"
fi

if [[ -n "${DB_MODE:-}" ]]; then
  echo -e "  Using DB_MODE=${DB_MODE} from environment"
else
  echo -n "  Choose [1/2] (default: 1): "
  read -r DB_MODE_INPUT < /dev/tty
  DB_MODE="${DB_MODE_INPUT:-1}"
fi

if [[ "$DB_MODE" != "1" && "$DB_MODE" != "2" ]]; then
  err "Invalid DB_MODE '$DB_MODE'. Must be 1 or 2."
  exit 1
fi

if [[ "$DB_MODE" == "2" ]]; then
  if [[ "$LAB_MODE" == "2" ]]; then
    # Real NetBird SQLite: checkpoint init uses an Alpine container — no host sqlite3 needed
    # NetBird stores activity events in events.db (not store.db which holds peers/accounts)
    EP_SQLITE_PATH="/var/lib/netbird/events.db"
    EP_SQLITE_HOST_PATH="./data/netbird/events.db"
    log "Database: SQLite — will read from NetBird's events.db"
  else
    if ! command -v sqlite3 &>/dev/null; then
      err "sqlite3 is required for simulated SQLite mode"
      err "Install with: sudo dnf install sqlite  (Rocky/RHEL)  or  sudo apt install sqlite3  (Debian/Ubuntu)"
      exit 1
    fi
    EP_SQLITE_PATH="/var/lib/netbird/store.db"
    EP_SQLITE_HOST_PATH="./data/sqlite/store.db"
    log "Database: SQLite — will create local store.db with stub data"
  fi
  export EP_SQLITE_HOST_PATH
else
  EP_SQLITE_PATH=""
  EP_SQLITE_HOST_PATH=""
  log "Database: PostgreSQL"
fi
echo ""

# ============================================================================
# 4. Create data directories
# ============================================================================
log "Creating data directories..."
mkdir -p \
  "$SCRIPT_DIR/data/postgres" \
  "$SCRIPT_DIR/data/eventsproc" \
  "$SCRIPT_DIR/data/sqlite" \
  "$SCRIPT_DIR/data/loki" \
  "$SCRIPT_DIR/data/grafana" \
  "$SCRIPT_DIR/data/splunk/var" \
  "$SCRIPT_DIR/data/splunk/etc" \
  "$SCRIPT_DIR/data/netbird"

# Pre-create config.yaml as an empty file so Docker bind-mounts it as a file
# rather than creating a root-owned directory in its place (Docker's default
# behaviour when the bind-mount source path does not exist).
touch "$SCRIPT_DIR/data/netbird/config.yaml"

# ============================================================================
# 5. Generate eventsproc config.yaml
# ============================================================================
log "Generating eventsproc config.yaml..."
if [[ "$DB_MODE" == "2" ]]; then
  # SQLite mode — reads from NetBird's built-in store mounted at /var/lib/netbird/store.db
  cat > "$SCRIPT_DIR/data/eventsproc/config.yaml" <<EOF
# Auto-generated by lab-setup.sh — edit and restart eventsproc to apply changes
database_driver: "sqlite"
sqlite_path: "${EP_SQLITE_PATH}"
platform: "${EP_PLATFORM}"
region: "${EP_REGION}"
log_level: "${EP_LOG_LEVEL}"
batch_size: ${EP_BATCH_SIZE}
polling_interval: ${EP_POLLING_INTERVAL}
metrics_port: 2113
email_enrichment:
  enabled: true
  source: "${EP_EMAIL_ENRICHMENT_SOURCE}"
$([ -n "${EP_NETBIRD_CONFIG_PATH}" ] && echo "  netbird_config_path: \"${EP_NETBIRD_CONFIG_PATH}\"")
EOF
else
  # PostgreSQL mode (default)
  cat > "$SCRIPT_DIR/data/eventsproc/config.yaml" <<EOF
# Auto-generated by lab-setup.sh — edit and restart eventsproc to apply changes
database_driver: "postgres"
postgres_url: "user=${POSTGRES_USER} password=${POSTGRES_PASSWORD} dbname=${POSTGRES_DB} host=postgres port=5432 sslmode=disable"
platform: "${EP_PLATFORM}"
region: "${EP_REGION}"
log_level: "${EP_LOG_LEVEL}"
batch_size: ${EP_BATCH_SIZE}
polling_interval: ${EP_POLLING_INTERVAL}
metrics_port: 2113
email_enrichment:
  enabled: true
  source: "${EP_EMAIL_ENRICHMENT_SOURCE}"
$([ -n "${EP_NETBIRD_CONFIG_PATH}" ] && echo "  netbird_config_path: \"${EP_NETBIRD_CONFIG_PATH}\"")
EOF
fi
log "Config written to data/eventsproc/config.yaml"

# ============================================================================
# 6. Build eventsproc Docker image
# ============================================================================
log "Building eventsproc Docker image from ${REPO_ROOT}..."
docker build \
  -t eventsproc:lab \
  -f "$SCRIPT_DIR/Dockerfile" \
  "$REPO_ROOT" \
  2>&1 | tail -5
log "eventsproc image built"

# ============================================================================
# 7. Create shared Docker network
# ============================================================================
if ! docker network inspect lab-net &>/dev/null; then
  log "Creating lab-net Docker network..."
  docker network create lab-net
else
  log "lab-net network already exists"
fi

# ============================================================================
# 8. Start lab services
# ============================================================================
COMPOSE_FILES="-f $SCRIPT_DIR/docker-compose.lab.yml"
if [[ "$DB_MODE" == "2" ]]; then
  if [[ "$LAB_MODE" == "2" ]]; then
    # Real NetBird SQLite: netbird_netbird_data volume doesn't exist yet (NetBird starts later).
    # Start lab services without the sqlite-netbird overlay; eventsproc will be recreated
    # with the named volume after NetBird is running (step 13).
    log "Starting lab services (PostgreSQL, Loki, Grafana, Splunk, Alloy, eventsproc [SQLite/deferred])..."
  else
    # Simulated SQLite: store.db is a local bind-mount path — volume exists immediately
    COMPOSE_FILES="$COMPOSE_FILES -f $SCRIPT_DIR/docker-compose.sqlite.yml"
    log "Starting lab services (PostgreSQL, Loki, Grafana, Splunk, Alloy, eventsproc [SQLite])..."
  fi
else
  log "Starting lab services (PostgreSQL, Loki, Grafana, Splunk, Alloy, eventsproc [PostgreSQL])..."
fi
# shellcheck disable=SC2086
docker compose $COMPOSE_FILES up -d --build

# ============================================================================
# 9. Wait for PostgreSQL
# ============================================================================
log "Waiting for PostgreSQL to be healthy..."
RETRIES=30
until docker exec lab-postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" &>/dev/null; do
  RETRIES=$((RETRIES - 1))
  if [[ $RETRIES -le 0 ]]; then
    err "PostgreSQL failed to start"
    exit 1
  fi
  sleep 2
done
log "PostgreSQL is ready"

# ============================================================================
# 10. Run init-db.sql (checkpoint table — always needed)
# ============================================================================
log "Initializing checkpoint table..."
docker exec -i lab-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" < "$SCRIPT_DIR/init-db.sql"
log "Checkpoint table ready"

# ============================================================================
# 11a. SIMULATED MODE — load stub tables with test events
# ============================================================================
if [[ "$LAB_MODE" == "1" ]]; then
  if [[ "$DB_MODE" == "2" ]]; then
    # Simulated + SQLite: init a standalone store.db with stub events
    log "Initializing SQLite store.db with stub data..."
    sqlite3 "$SCRIPT_DIR/data/sqlite/store.db" < "$SCRIPT_DIR/init-stub-sqlite.sql"
    EVENT_COUNT=$(sqlite3 "$SCRIPT_DIR/data/sqlite/store.db" "SELECT COUNT(*) FROM events;")
    log "SQLite stub data loaded — ${EVENT_COUNT} events ready in data/sqlite/store.db"
    log "eventsproc will pick them up on the next poll (within ${EP_POLLING_INTERVAL}s)"
  else
    # Simulated + PostgreSQL (default): load stub data via psql
    log "Loading stub NetBird tables and test events into PostgreSQL..."
    docker exec -i lab-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" < "$SCRIPT_DIR/init-stub-data.sql"
    EVENT_COUNT=$(docker exec lab-postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -c "SELECT COUNT(*) FROM events;" 2>/dev/null | tr -d ' ')
    log "Stub data loaded — ${EVENT_COUNT} events ready"
    log "eventsproc will pick them up on the next poll (within ${EP_POLLING_INTERVAL}s)"
  fi

# ============================================================================
# 11b. REAL NETBIRD MODE — run getting-started.sh + patch for Cloudflare
# ============================================================================
else
  # Resolve NETBIRD_REPO
  if [[ -n "$NETBIRD_REPO" ]]; then
    NETBIRD_REPO="${NETBIRD_REPO/#\~/$HOME}"
  fi

  if [[ -z "$NETBIRD_REPO" ]]; then
    log "NETBIRD_REPO not set — searching common locations..."
    for candidate in \
      "$REPO_ROOT/../netbird" \
      "$HOME/netbird" \
      "$HOME/git/netbird" \
      "$HOME/git/local/netbird"; do
      if [[ -f "$candidate/infrastructure_files/getting-started.sh" ]]; then
        NETBIRD_REPO="$candidate"
        log "Found NetBird repo at $NETBIRD_REPO"
        break
      fi
    done
  fi

  if [[ -z "$NETBIRD_REPO" ]]; then
    warn "NetBird repo not found automatically."
    warn "Clone it with: git clone https://github.com/netbirdio/netbird.git ~/netbird"
    echo -n "Enter path to NetBird repo (or press Enter to skip): "
    read -r NETBIRD_REPO_INPUT < /dev/tty
    NETBIRD_REPO="${NETBIRD_REPO_INPUT/#\~/$HOME}"
  fi

  GETTING_STARTED=""
  if [[ -n "$NETBIRD_REPO" && -f "$NETBIRD_REPO/infrastructure_files/getting-started.sh" ]]; then
    GETTING_STARTED="$NETBIRD_REPO/infrastructure_files/getting-started.sh"
  fi

  if [[ -z "$GETTING_STARTED" ]]; then
    err "NetBird getting-started.sh not found — cannot continue in real NetBird mode"
    exit 1
  fi

  log "Setting up NetBird from $NETBIRD_REPO..."

  # Patch getting-started.sh to use env vars instead of /dev/tty reads.
  # Two-step: (1) init_config() hard-resets vars — patch those to use ${VAR:-default}
  #           (2) call sites overwrite with function reads — patch to ${VAR:-$(func)}
  PATCHED_SCRIPT=$(mktemp)
  cp "$GETTING_STARTED" "$PATCHED_SCRIPT"

  # Step 1: patch init_config() default assignments
  sed -i 's/REVERSE_PROXY_TYPE="0"/REVERSE_PROXY_TYPE="${REVERSE_PROXY_TYPE:-0}"/' "$PATCHED_SCRIPT"
  sed -i 's/TRAEFIK_ACME_EMAIL=""/TRAEFIK_ACME_EMAIL="${TRAEFIK_ACME_EMAIL:-}"/' "$PATCHED_SCRIPT"
  sed -i 's/ENABLE_PROXY="false"/ENABLE_PROXY="${ENABLE_PROXY:-false}"/' "$PATCHED_SCRIPT"

  # Step 2: patch call sites so set env vars skip the /dev/tty reader functions
  sed -i 's/REVERSE_PROXY_TYPE=$(read_reverse_proxy_type)/REVERSE_PROXY_TYPE="${REVERSE_PROXY_TYPE:-$(read_reverse_proxy_type)}"/' "$PATCHED_SCRIPT"
  sed -i 's/TRAEFIK_ACME_EMAIL=$(read_traefik_acme_email)/TRAEFIK_ACME_EMAIL="${TRAEFIK_ACME_EMAIL:-$(read_traefik_acme_email)}"/' "$PATCHED_SCRIPT"
  sed -i 's/ENABLE_PROXY=$(read_enable_proxy)/ENABLE_PROXY="${ENABLE_PROXY:-$(read_enable_proxy)}"/' "$PATCHED_SCRIPT"

  export NETBIRD_DOMAIN REVERSE_PROXY_TYPE TRAEFIK_ACME_EMAIL ENABLE_PROXY

  NETBIRD_DIR="$SCRIPT_DIR/data/netbird"
  mkdir -p "$NETBIRD_DIR"
  cd "$NETBIRD_DIR"

  # Remove the empty placeholder so getting-started.sh can write its config files.
  # (The placeholder was created in step 4 to prevent Docker from bind-mounting
  # the path as a root-owned directory; it has served its purpose.)
  rm -f "$NETBIRD_DIR/config.yaml"

  log "Running NetBird getting-started.sh (non-interactive)..."
  bash "$PATCHED_SCRIPT" || warn "getting-started.sh exited with error — continuing"
  rm -f "$PATCHED_SCRIPT" "$PATCHED_SCRIPT.bak"
  cd "$SCRIPT_DIR"

  # ============================================================================
  # 12. Patch NetBird docker-compose.yml
  #     - shared lab-net network
  #     - PostgreSQL instead of SQLite
  #     - Traefik: Cloudflare DNS challenge
  # ============================================================================
  NB_COMPOSE="$NETBIRD_DIR/docker-compose.yml"
  if [[ ! -f "$NB_COMPOSE" ]]; then
    err "NetBird docker-compose.yml not generated — check getting-started.sh output"
    exit 1
  fi

  log "Patching NetBird docker-compose.yml..."

  if ! command -v yq &>/dev/null; then
    err "yq is required for patching. Install: snap install yq"
    exit 1
  fi

  # --- 12a. Add lab-net network ---
  if ! yq -e '.networks.lab-net' "$NB_COMPOSE" &>/dev/null; then
    # Add lab-net to top-level networks block (merge, not append)
    yq -i '.networks.lab-net.external = true' "$NB_COMPOSE"
    # Add lab-net per service — Traefik uses map-style (ipv4_address), others list-style
    for svc in $(yq '.services | keys | .[]' "$NB_COMPOSE"); do
      net_type=$(yq ".services.${svc}.networks | type" "$NB_COMPOSE" 2>/dev/null || echo "!!null")
      if [[ "$net_type" == "!!map" ]]; then
        yq -i ".services.${svc}.networks.lab-net = {}" "$NB_COMPOSE"
      else
        yq -i ".services.${svc}.networks += [\"lab-net\"]" "$NB_COMPOSE"
      fi
    done
    log "Added lab-net to NetBird services"
  fi

  # --- 12b. Database configuration for NetBird ---
  if [[ "$DB_MODE" == "2" ]]; then
    # SQLite mode: NetBird uses its built-in SQLite store (default, no patching needed)
    log "SQLite mode: NetBird will use its built-in store.db (no PostgreSQL patch needed)"
  else
    # PostgreSQL mode: Patch config.yaml store engine (nested under .server.store)
    NB_CONFIG="$NETBIRD_DIR/config.yaml"
    if [[ -f "$NB_CONFIG" ]]; then
      log "Patching NetBird config.yaml for PostgreSQL store..."
      yq -i '.server.store.engine = "postgres"' "$NB_CONFIG"
    fi

    # Set env vars for both the main store and the activity event store
    # (activity events = the 'events' table that eventsproc reads)
    POSTGRES_DSN="host=lab-postgres user=${POSTGRES_USER} password=${POSTGRES_PASSWORD} dbname=${POSTGRES_DB} port=5432 sslmode=disable"
    export NB_STORE_ENGINE_POSTGRES_DSN="$POSTGRES_DSN"
    export NB_ACTIVITY_EVENT_POSTGRES_DSN="$POSTGRES_DSN"
    yq -i '.services.netbird-server.environment.NB_STORE_ENGINE_POSTGRES_DSN = env(NB_STORE_ENGINE_POSTGRES_DSN)' "$NB_COMPOSE"
    yq -i '.services.netbird-server.environment.NB_ACTIVITY_EVENT_STORE_ENGINE = "postgres"' "$NB_COMPOSE"
    yq -i '.services.netbird-server.environment.NB_ACTIVITY_EVENT_POSTGRES_DSN = env(NB_ACTIVITY_EVENT_POSTGRES_DSN)' "$NB_COMPOSE"
    log "NetBird configured to use shared PostgreSQL (management + activity events)"
  fi

  # --- 12c. Traefik: Cloudflare DNS challenge ---
  log "Patching Traefik for Cloudflare DNS challenge..."

  # Copy cf-token into NetBird dir as a Docker secret file
  cp "$SCRIPT_DIR/secrets/cf-token" "$NETBIRD_DIR/cf-token"

  # Add Docker secret definition (idempotent)
  if ! yq -e '.secrets.cf-token' "$NB_COMPOSE" &>/dev/null; then
    yq -i '.secrets.cf-token.file = "./cf-token"' "$NB_COMPOSE"
  fi

  # Mount secret + set CF_DNS_API_TOKEN_FILE on Traefik (same approach as SecOps)
  if ! yq -e '.services.traefik.secrets[] | select(. == "cf-token")' "$NB_COMPOSE" &>/dev/null 2>&1; then
    yq -i '.services.traefik.secrets += ["cf-token"]' "$NB_COMPOSE"
  fi
  yq -i '.services.traefik.environment.CF_DNS_API_TOKEN_FILE = "/run/secrets/cf-token"' "$NB_COMPOSE"

  # Replace TLS/HTTP challenge with Cloudflare DNS challenge in Traefik command args
  # Detect resolver name from existing args (NetBird uses 'letsencrypt')
  NB_RESOLVER=$(yq '.services.traefik.command[] | select(test("certificatesresolvers\\..*\\.acme\\.email"))' "$NB_COMPOSE" \
    | sed 's/--certificatesresolvers\.\(.*\)\.acme\.email=.*/\1/')
  NB_RESOLVER="${NB_RESOLVER:-letsencrypt}"
  log "Traefik certificate resolver name: $NB_RESOLVER"

  # Remove TLS/HTTP/DNS challenge args (idempotent)
  yq -i '.services.traefik.command = (.services.traefik.command | map(select(test("acme\\.tlschallenge|acme\\.httpchallenge") | not)))' "$NB_COMPOSE"
  yq -i '.services.traefik.command = (.services.traefik.command | map(select(test("dnschallenge|caserver") | not)))' "$NB_COMPOSE"

  # Add Cloudflare DNS challenge args using detected resolver name
  yq -i ".services.traefik.command += [
    \"--certificatesresolvers.${NB_RESOLVER}.acme.dnschallenge=true\",
    \"--certificatesresolvers.${NB_RESOLVER}.acme.dnschallenge.provider=cloudflare\",
    \"--certificatesresolvers.${NB_RESOLVER}.acme.dnschallenge.resolvers=1.1.1.1:53,1.0.0.1:53\",
    \"--certificatesresolvers.${NB_RESOLVER}.acme.dnschallenge.propagation.delayBeforeChecks=30s\"
  ]" "$NB_COMPOSE"

  # Optionally switch to Let's Encrypt staging (no rate limits — certs not trusted by browsers)
  # Set ACME_STAGING=1 in lab.env to use staging while developing/testing.
  if [[ "${ACME_STAGING:-0}" == "1" ]]; then
    log "Using Let's Encrypt STAGING CA (certs will not be browser-trusted)"
    yq -i ".services.traefik.command += [
      \"--certificatesresolvers.${NB_RESOLVER}.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory\"
    ]" "$NB_COMPOSE"
  fi

  log "Traefik patched for Cloudflare DNS challenge"

  # --- 12d. Add wildcard cert domains to all HTTPS routers (SecOps pattern) ---
  # Extracts the base domain (last two labels) so a single *.base cert covers all
  # subdomains — mirrors the tls.domains[0].main/sans pattern used in SecOps/Traefikv3.
  # e.g. NETBIRD_DOMAIN=netbird.example.com  →  NETBIRD_BASE_DOMAIN=example.com
  NETBIRD_BASE_DOMAIN=$(echo "$NETBIRD_DOMAIN" | awk -F. '{print $(NF-1)"."$NF}')
  log "Adding wildcard cert domains (main=${NETBIRD_BASE_DOMAIN}, sans=*.${NETBIRD_BASE_DOMAIN})..."

  for svc in $(yq '.services | keys | .[]' "$NB_COMPOSE"); do
    labels_type=$(yq ".services.${svc}.labels | type" "$NB_COMPOSE" 2>/dev/null || echo "!!null")
    [[ "$labels_type" == "!!null" ]] && continue
    while IFS= read -r rname; do
      [[ -z "$rname" ]] && continue
      # Remove any pre-existing tls.domains entries for this router (idempotent)
      yq -i ".services.${svc}.labels = (.services.${svc}.labels | map(select(test(\"traefik.http.routers\\.${rname}\\.tls\\.domains\") | not)))" "$NB_COMPOSE"
      # Add wildcard cert domains matching SecOps pattern
      yq -i ".services.${svc}.labels += [
        \"traefik.http.routers.${rname}.tls.domains[0].main=${NETBIRD_BASE_DOMAIN}\",
        \"traefik.http.routers.${rname}.tls.domains[0].sans=*.${NETBIRD_BASE_DOMAIN}\"
      ]" "$NB_COMPOSE"
    done < <(yq ".services.${svc}.labels[] | select(test(\"traefik.http.routers\\..*\\.tls\\.certresolver\")) | capture(\"traefik.http.routers.(?P<r>[^.]+)\\.tls\\.certresolver\").r" "$NB_COMPOSE" 2>/dev/null | sort -u)
  done
  log "Wildcard cert domains added"

  # ============================================================================
  # 13. Start NetBird
  # ============================================================================
  log "Starting NetBird services..."
  cd "$NETBIRD_DIR"
  docker compose up -d
  cd "$SCRIPT_DIR"

  log "Waiting for NetBird to be healthy..."
  RETRIES=30
  until curl -sf http://localhost/api/health &>/dev/null; do
    RETRIES=$((RETRIES - 1))
    if [[ $RETRIES -le 0 ]]; then
      warn "NetBird health check timed out — it may still be starting"
      break
    fi
    sleep 3
  done

  # Recreate/restart eventsproc now that NetBird is running.
  # - In SQLite mode: netbird_netbird_data volume now exists; recreate eventsproc with
  #   the sqlite-netbird overlay so it gets the named volume mount.
  # - In PostgreSQL mode: restart to pick up the real NetBird config.yaml (encryption key).
  if [[ "$DB_MODE" == "2" ]]; then
    log "Recreating eventsproc with netbird_netbird_data volume (SQLite mode)..."
    docker compose -f "$SCRIPT_DIR/docker-compose.lab.yml" \
      -f "$SCRIPT_DIR/docker-compose.sqlite-netbird.yml" \
      up -d --no-deps eventsproc
  else
    log "Restarting eventsproc to load NetBird encryption key..."
    docker restart lab-eventsproc
  fi

  # --- SQLite: add checkpoint table to NetBird's events.db ---
  # NetBird stores activity events in events.db (separate from store.db which holds
  # peers/accounts). The checkpoint table must live in the same database as events.
  if [[ "$DB_MODE" == "2" ]]; then
    log "Waiting for NetBird events.db to be created..."
    RETRIES=20
    until docker exec netbird-server test -f /var/lib/netbird/events.db &>/dev/null; do
      RETRIES=$((RETRIES - 1))
      if [[ $RETRIES -le 0 ]]; then
        warn "events.db not found inside netbird-server — skipping checkpoint init"
        break
      fi
      sleep 2
    done
    if docker exec netbird-server test -f /var/lib/netbird/events.db &>/dev/null; then
      log "Adding eventsproc checkpoint table to NetBird events.db..."
      # events.db lives in the netbird_netbird_data named volume; init via a
      # temporary Alpine container with sqlite3 rather than the host path.
      docker run --rm \
        -v netbird_netbird_data:/data \
        -v "$SCRIPT_DIR/init-sqlite.sql":/init.sql:ro \
        alpine sh -c "apk add --no-cache sqlite &>/dev/null && sqlite3 /data/events.db < /init.sql"
      log "Checkpoint table ready in events.db"
    fi
  fi
fi

# ============================================================================
# 14. Print summary
# ============================================================================
SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")

echo ""
echo -e "${BLUE}============================================================================${NC}"
echo -e "${BLUE}  Lab Setup Complete${NC}"
echo -e "${BLUE}============================================================================${NC}"
echo ""

echo -e "  ${GREEN}Lab mode:${NC} $([ "$LAB_MODE" == "1" ] && echo "Simulated (stub data)" || echo "Real NetBird")"
echo -e "  ${GREEN}Database:${NC} $([ "$DB_MODE" == "2" ] && echo "SQLite (built-in store at ${EP_SQLITE_HOST_PATH})" || echo "PostgreSQL")"
echo ""
echo -e "  ${GREEN}Services:${NC}"
if [[ "$LAB_MODE" == "2" ]]; then
  echo -e "    NetBird Dashboard:   http://${SERVER_IP}"
fi
echo -e "    Grafana:             http://${SERVER_IP}:${GRAFANA_PORT}  (admin/${GRAFANA_ADMIN_PASSWORD})"
echo -e "    Splunk:              http://${SERVER_IP}:${SPLUNK_PORT}  (admin/${SPLUNK_PASSWORD})"
echo -e "    Loki:                http://${SERVER_IP}:${LOKI_PORT}"
echo -e "    eventsproc metrics:  http://${SERVER_IP}:${EP_METRICS_PORT}/metrics"
echo ""
echo -e "  ${GREEN}Next steps:${NC}"
if [[ "$LAB_MODE" == "1" ]]; then
  echo -e "    1. Watch eventsproc process stub events: docker logs -f lab-eventsproc"
  echo -e "    2. Grafana → Explore → Loki → {service_name=\"lab-eventsproc\"}"
  echo -e "    3. Splunk → search: index=main source=\"eventsproc\""
  if [[ "$DB_MODE" == "2" ]]; then
    echo -e "    4. Add more events: sqlite3 data/sqlite/store.db"
    echo -e "       INSERT INTO events (timestamp, activity, initiator_id, account_id)"
    echo -e "         VALUES (datetime('now'), 1, 'user-alice', 'account-001');"
  else
    echo -e "    4. Add more events: docker exec -i lab-postgres psql -U netbird -d netbird"
  fi
else
  echo -e "    1. Open NetBird dashboard and create an admin account"
  echo -e "    2. Create a test user and peer to generate real events"
  echo -e "    3. eventsproc picks them up within ${EP_POLLING_INTERVAL}s"
fi
echo ""
echo -e "  ${GREEN}Useful commands:${NC}"
echo -e "    View eventsproc logs:  docker logs -f lab-eventsproc"
echo -e "    Edit config:           \$EDITOR data/eventsproc/config.yaml"
echo -e "    Restart eventsproc:    docker restart lab-eventsproc"
echo -e "    Command reference:     cat COMMANDS.md"
echo -e "    Tear down everything:  ./lab-teardown.sh"
echo ""
