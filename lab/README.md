# netbird-events Lab

A self-contained Docker environment for evaluating **eventsproc** — the audit event exporter for [NetBird](https://netbird.io). Run it against simulated data or your own live NetBird instance and see events flowing into Grafana and Splunk in minutes.

---

## What You Get

| Container | Purpose | Port |
|-----------|---------|------|
| `lab-eventsproc` | Reads NetBird audit events, ships to Loki + Splunk | 2113 (metrics) |
| `lab-postgres` | PostgreSQL — shared by NetBird + eventsproc | 5432 (internal) |
| `lab-loki` | Log storage backend | 3100 (internal) |
| `lab-grafana` | Dashboards — Loki + Prometheus pre-provisioned | 3000 |
| `lab-splunk` | Log search + HEC ingest | 8000 |
| `lab-alloy` | Collects container logs → Loki + Splunk | — |
| NetBird stack | Traefik + Management + Signal + Relay | 80, 443, 3478 |

The NetBird stack is only started in **Real NetBird mode** (option 2). In simulated mode you get everything except NetBird itself — eventsproc processes realistic stub events instead.

---

## Prerequisites

| Requirement | Simulated mode | Real NetBird mode |
|-------------|:--------------:|:-----------------:|
| Linux server with Docker | ✅ | ✅ |
| 4 GB RAM, 20 GB disk | ✅ | ✅ |
| Ports 80 + 443 open to the internet | — | ✅ |
| Domain managed by Cloudflare | — | ✅ |
| Cloudflare API token (`Zone / DNS / Edit`) | — | ✅ |
| NetBird repo clone (`~/netbird`) | — | ✅ |

**If you just want to see events flow — use simulated mode. No domain or cloud account needed.**

### Software

Everything below is installed automatically by `bootstrap-rocky.sh` on a fresh machine. For existing servers you need: **Docker Engine**, **jq**, **yq**.

---

## Quick Start

### Fastest path — simulated events, no domain needed (~5 min)

```bash
# 1. Clone the repo on your server
git clone https://github.com/xh63/netbird-events.git ~/netbird-events

# 2. Run the lab in simulated mode (no prompts, no domain required)
cd ~/netbird-events/lab
LAB_MODE=1 ./lab-setup.sh

# 3. Open Grafana — events appear within 30 seconds
#    http://YOUR_SERVER_IP:3000  (admin / admin)
#    Explore → Loki → label filter: service_name = lab-eventsproc
```

---

### Step 1: Bootstrap the Server *(fresh Rocky Linux 10 only)*

The bootstrap script runs **as root** on the server. It creates your user, configures SSH key auth, grants passwordless sudo, installs all tools, and clones both repos.

**Option A — pipe from your Mac (recommended):**

```bash
ssh root@YOUR_SERVER_IP 'bash -s' < lab/bootstrap-rocky.sh -- \
  --user YOUR_USER \
  --ssh-key "$(cat ~/.ssh/id_ed25519.pub)"
```

**Option B — run directly on the server:**

```bash
sudo bash bootstrap-rocky.sh \
  --user YOUR_USER \
  --ssh-key "ssh-ed25519 AAAA...your-key..."
```

After bootstrap, log in as your user — `~/netbird-events` is already cloned and ready.

---

### Step 2: Place the Cloudflare Token *(Real NetBird mode only)*

Create a Cloudflare API token with `Zone / DNS / Edit` permission:

1. [Cloudflare dashboard](https://dash.cloudflare.com) → **My Profile** → **API Tokens** → **Create Token**
2. Click **Create Custom Token** and set:
   - `Zone` / `Zone` / `Read`
   - `Zone` / `DNS` / `Edit`
3. Under **Zone Resources**: select your domain → **Create Token** — copy the token (shown once)

Place the token on the server:

```bash
# From your Mac:
scp /path/to/cf-token  YOUR_USER@YOUR_SERVER_IP:~/netbird-events/lab/secrets/cf-token

# Or create it directly on the server (no quotes, no trailing newline):
printf '%s' 'YOUR_TOKEN_HERE' > ~/netbird-events/lab/secrets/cf-token
```

The `secrets/` directory is gitignored — your token is never committed.

---

### Step 3: Configure

```bash
cd ~/netbird-events/lab
cp lab.env.example lab.env
vi lab.env
```

Key settings to review:

| Variable | What to set |
|----------|-------------|
| `NETBIRD_DOMAIN` | Subdomain pointing at this server — e.g. `netbird.yourdomain.com`. Must match a real DNS A record. **Real NetBird mode only.** |
| `TRAEFIK_ACME_EMAIL` | Your email for Let's Encrypt certificate notifications. **Real NetBird mode only.** |

Everything else has sensible defaults and works out of the box.

---

### Step 4: Run Setup

The setup script asks two questions — or skip them with env vars:

```bash
# Interactive:
./lab-setup.sh

# Non-interactive shortcuts:
LAB_MODE=1 ./lab-setup.sh               # Simulated + PostgreSQL  (quickest trial)
LAB_MODE=1 DB_MODE=2 ./lab-setup.sh     # Simulated + SQLite
LAB_MODE=2 DB_MODE=1 ./lab-setup.sh     # Real NetBird + PostgreSQL  (production-like)
LAB_MODE=2 DB_MODE=2 ./lab-setup.sh     # Real NetBird + SQLite (reads NetBird's store.db)
```

**Lab mode — where do events come from?**

| Option | Description | What you need |
|--------|-------------|---------------|
| `1` Simulated (default) | Realistic stub events pre-loaded into the DB | Nothing extra |
| `2` Real NetBird | Full stack: NetBird + Traefik + Cloudflare TLS | Domain + CF token + NetBird repo |

**Database mode — how does eventsproc read events?**

| Option | Description | Notes |
|--------|-------------|-------|
| `1` PostgreSQL (default) | Managed Postgres shared by NetBird + eventsproc | Required for cluster/HA |
| `2` SQLite | NetBird's built-in file store | No Postgres needed; no HA support |

SQLite mode requires `sqlite3` on the host:

```bash
sudo dnf install sqlite    # Rocky/RHEL
sudo apt install sqlite3   # Debian/Ubuntu
```

---

### Step 5: Access Services

| Service | URL | Default credentials |
|---------|-----|---------------------|
| Grafana | `http://YOUR_SERVER_IP:3000` | admin / admin |
| Splunk | `http://YOUR_SERVER_IP:8000` | admin / changeme123 |
| NetBird dashboard | `https://NETBIRD_DOMAIN` | create on first visit |
| eventsproc metrics | `http://YOUR_SERVER_IP:2113/metrics` | — |

> **Real NetBird mode:** your browser must resolve `NETBIRD_DOMAIN`. If the server is on a private network, add a hosts entry on your Mac/PC:
>
> ```
> YOUR_SERVER_IP  netbird.yourdomain.com
> ```

---

## What You'll See

Within 30 seconds of setup, eventsproc starts emitting structured JSON audit events:

```json
{
  "event_id": 42,
  "timestamp": "2026-03-06T09:15:00Z",
  "activity_code": "user.login",
  "activity_name": "User Login",
  "initiator_email": "alice@example.com",
  "account_id": "account-001",
  "meta": {"ip": "203.0.113.10"}
}
```

**Grafana:** Explore → Loki → label filter `service_name = lab-eventsproc`

**Splunk:** Search → `index=main source="eventsproc"`

**Live container logs:**

```bash
docker logs -f lab-eventsproc
```

---

## Generating More Test Events

### Simulated mode — PostgreSQL

```bash
docker exec -it lab-postgres psql -U netbird -d netbird -c "
INSERT INTO events (timestamp, activity, initiator_id, target_id, account_id, meta)
VALUES (NOW(), 1, 'user-alice', 'user-alice', 'account-001', '{\"ip\":\"203.0.113.10\"}');"
```

### Simulated mode — SQLite

```bash
sqlite3 data/sqlite/store.db \
  "INSERT INTO events (timestamp, activity, initiator_id, account_id)
   VALUES (datetime('now'), 7, 'user-alice', 'account-001');"
```

### Real NetBird mode

Open the NetBird dashboard, create users and peers — events are generated automatically.

---

## Teardown

```bash
./lab-teardown.sh
```

Destroys all containers, volumes, and runtime data. The `secrets/` directory (and your CF token) is preserved.

To rebuild from scratch:

```bash
./lab-teardown.sh && ./lab-setup.sh
```

---

## File Structure

```
lab/
├── bootstrap-rocky.sh        # Bootstrap a fresh Rocky Linux server
├── lab-setup.sh              # Start the lab (prompts for lab + DB mode)
├── lab-teardown.sh           # Stop and destroy everything
├── lab.env.example           # Configuration template (copy to lab.env)
├── Dockerfile                # Multi-stage eventsproc image build
├── docker-compose.lab.yml    # Lab services (PostgreSQL, Loki, Grafana, Splunk, Alloy)
├── docker-compose.sqlite.yml # Compose override — SQLite mode
├── init-db.sql               # PostgreSQL: checkpoint table schema
├── init-stub-data.sql        # PostgreSQL: stub events for simulated mode
├── init-sqlite.sql           # SQLite: checkpoint table schema
├── init-stub-sqlite.sql      # SQLite: stub events for simulated SQLite mode
├── configs/
│   ├── loki-config.yml
│   ├── alloy-config.alloy
│   └── grafana/
│       ├── datasources.yml
│       └── dashboards.yml
├── secrets/                  # Gitignored — put cf-token here
│   └── cf-token.example
└── data/                     # Gitignored — runtime volumes
    ├── postgres/
    ├── sqlite/
    ├── eventsproc/           # Auto-generated config.yaml
    ├── loki/
    ├── grafana/
    ├── splunk/
    └── netbird/
```

---

## Troubleshooting

### eventsproc exits immediately

`polling_interval` is `0` (run-once mode). Fix:

```bash
vi data/eventsproc/config.yaml   # set polling_interval: 30
docker restart lab-eventsproc
```

### Port already in use

Edit `lab.env` (e.g. change `GRAFANA_PORT` to `3001`) then rebuild:

```bash
./lab-teardown.sh && ./lab-setup.sh
```

### Let's Encrypt certificate not issued

- Confirm `secrets/cf-token` contains only the token string — no quotes, no trailing newline
- Confirm the token has `Zone / DNS / Edit` permission for your domain
- Ports 80 and 443 must be reachable from the internet
- Allow ~60 seconds after setup for the DNS challenge to complete
- Check Traefik logs: `docker logs netbird-traefik 2>&1 | grep -i acme`

### NetBird repo not found

```bash
git clone https://github.com/netbirdio/netbird.git ~/netbird
./lab-teardown.sh && LAB_MODE=2 ./lab-setup.sh
```
