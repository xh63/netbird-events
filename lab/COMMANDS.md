# Lab Command Reference

Useful commands for operating the netbird-events lab. Replace `$SERVER_IP` and `$NETBIRD_DOMAIN` with your values.

---

## Bootstrap (fresh Rocky Linux 10)

```bash
# From your Mac — creates user, installs tools, clones repos
ssh root@$SERVER_IP 'bash -s' < lab/bootstrap-rocky.sh -- \
  --user YOUR_USER \
  --ssh-key "$(cat ~/.ssh/id_ed25519.pub)"

# Verify after bootstrap
docker --version
docker compose version
yq --version
jq --version
```

---

## Lab Setup

```bash
cd ~/netbird-events/lab

# Copy Cloudflare token (Real NetBird mode only, from your Mac)
scp /path/to/cf-token  YOUR_USER@$SERVER_IP:~/netbird-events/lab/secrets/cf-token

# Configure
cp lab.env.example lab.env
vi lab.env   # set NETBIRD_DOMAIN and TRAEFIK_ACME_EMAIL

# Run setup — choose a mode:
LAB_MODE=1 ./lab-setup.sh               # Simulated + PostgreSQL (quickest)
LAB_MODE=1 DB_MODE=2 ./lab-setup.sh     # Simulated + SQLite
LAB_MODE=2 DB_MODE=1 ./lab-setup.sh     # Real NetBird + PostgreSQL
LAB_MODE=2 DB_MODE=2 ./lab-setup.sh     # Real NetBird + SQLite
```

---

## Day-to-Day Operations

```bash
# Container status
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

# Live eventsproc logs
docker logs -f lab-eventsproc

# Edit eventsproc config and restart
vi ~/netbird-events/lab/data/eventsproc/config.yaml
docker restart lab-eventsproc

# Check processing checkpoint
docker exec lab-postgres psql -U netbird -d netbird \
  -c "SELECT * FROM idp.event_processing_checkpoint;"

# Restart all lab services
cd ~/netbird-events/lab
docker compose -f docker-compose.lab.yml restart

# Full teardown and rebuild
cd ~/netbird-events/lab
./lab-teardown.sh && git pull && ./lab-setup.sh
```

---

## Inserting Test Events

### PostgreSQL (simulated or real NetBird mode)

```bash
docker exec -i lab-postgres psql -U netbird -d netbird <<SQL
INSERT INTO events (timestamp, activity, initiator_id, target_id, account_id, meta)
VALUES (NOW(), 1, 'user-alice', 'user-alice', 'account-001', '{"ip":"203.0.113.10"}');
SQL
```

### SQLite (simulated SQLite mode)

```bash
sqlite3 ~/netbird-events/lab/data/sqlite/store.db \
  "INSERT INTO events (timestamp, activity, initiator_id, account_id)
   VALUES (datetime('now'), 7, 'user-alice', 'account-001');"
```

---

## Real NetBird — Accessing the Dashboard

NetBird is served at `https://$NETBIRD_DOMAIN` with a Let's Encrypt wildcard cert issued via Cloudflare DNS challenge.

```bash
# Option A: Add /etc/hosts entry on your Mac (recommended for LAN)
sudo sh -c 'echo "$SERVER_IP  $NETBIRD_DOMAIN" >> /etc/hosts'
# Then open: https://$NETBIRD_DOMAIN

# Option B: SSH port forwarding from your Mac
ssh -L 443:localhost:443 YOUR_USER@$SERVER_IP
# Then open: https://localhost  (cert warning expected — cert is for $NETBIRD_DOMAIN)

# Verify cert validity
echo | openssl s_client -connect $NETBIRD_DOMAIN:443 -servername $NETBIRD_DOMAIN 2>/dev/null \
  | openssl x509 -noout -subject -issuer -dates
```

### First-time dashboard setup

1. Open `https://$NETBIRD_DOMAIN` — create the admin account (first user gets admin)
2. Create test users and peers to generate real activity events
3. eventsproc picks them up within its polling interval (default: 30s)

---

## Traefik / TLS Troubleshooting

```bash
# Check Traefik cert status
docker logs netbird-traefik 2>&1 | grep -i 'acme\|cert\|error'

# Inspect stored certs
docker exec netbird-traefik cat /letsencrypt/acme.json \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
for r, v in d.items():
    for c in v.get('Certificates', []):
        print(r, c['domain'])
"

# Force cert re-issuance (only if cert is missing or corrupt)
docker exec netbird-traefik rm /letsencrypt/acme.json
cd ~/netbird-events/lab/data/netbird && docker compose restart traefik
```

---

## Database Inspection

```bash
# List all tables in PostgreSQL
docker exec lab-postgres psql -U netbird -d netbird -c '\dt' | head -20

# NetBird store engine (should show postgres in real mode)
cat ~/netbird-events/lab/data/netbird/config.yaml | grep -A5 store

# Count events
docker exec lab-postgres psql -U netbird -d netbird \
  -c "SELECT COUNT(*) FROM events;"

# Recent events
docker exec lab-postgres psql -U netbird -d netbird \
  -c "SELECT id, timestamp, activity FROM events ORDER BY timestamp DESC LIMIT 10;"
```

---

## Service URLs

| Service | URL | Credentials |
|---------|-----|-------------|
| Grafana | `http://$SERVER_IP:3000` | admin / admin |
| Splunk | `http://$SERVER_IP:8000` | admin / changeme123 |
| Loki | `http://$SERVER_IP:3100` | — |
| eventsproc metrics | `http://$SERVER_IP:2113/metrics` | — |
| NetBird dashboard | `https://$NETBIRD_DOMAIN` | set on first login |
| PostgreSQL | `$SERVER_IP:5432` | netbird / netbird-lab-pass |
