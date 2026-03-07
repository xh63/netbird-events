# NetBird Events Exporter

Export NetBird audit events from PostgreSQL or SQLite to centralized logging platforms (Loki, Splunk) via OpenTelemetry Collector.

## Overview

`eventsproc` is a lightweight Go service that:
- ✅ Reads events from NetBird's **PostgreSQL** or **SQLite** database
- ✅ Decrypts AES-GCM encrypted email/name fields (NetBird encrypts these at rest)
- ✅ Enriches events with user emails and activity names (configurable)
- ✅ Outputs structured JSON to stdout
- ✅ Forwards to Loki/Splunk via OpenTelemetry Collector
- ✅ Maintains checkpoints for reliable processing
- ✅ Supports High Availability (HA) deployments
- ✅ Works with both standard NetBird and custom deployments

## Architecture

```
PostgreSQL  ┐
            ├─→ eventsproc → stdout → systemd journal → OTEL Collector → Loki/Splunk
SQLite      ┘   (enriches)   (JSON)   (captures)        (forwards)       (stores)
```

**Key Design:**
- `eventsproc` outputs JSON to stdout only
- systemd captures logs to journal
- Grafana Alloy/OTEL Collector reads from journal and forwards to destinations
- Clean separation of concerns: processing vs forwarding

## Try it in the Lab

If you've cloned this repo, the best place to start is the lab — a self-contained Docker environment that gets you from zero to streaming audit events in under 5 minutes.

**No domain. No cloud account. Just Docker.**

```bash
git clone https://github.com/xh63/netbird-events.git ~/netbird-events
cd ~/netbird-events/lab
LAB_MODE=1 ./lab-setup.sh
```

Within 30 seconds, eventsproc is processing realistic stub events and shipping them to:

- **Grafana** at `http://YOUR_SERVER_IP:3000` — explore events in Loki
- **Splunk** at `http://YOUR_SERVER_IP:8000` — search `index=main source="eventsproc"`

The lab also supports a **real NetBird mode** that spins up the full NetBird stack (Management, Signal, Relay, Traefik with Let's Encrypt TLS) and lets you generate live events by adding users and peers through the dashboard. You can run it against PostgreSQL or NetBird's built-in SQLite store.

**Read the lab guide before you start — it covers all the options:**

**→ [lab/README.md](lab/README.md)**

---

## Quick Start

### 1. Install Binary

```bash
# Download or build
make build

# Install
sudo cp bin/eventsproc /usr/local/bin/
sudo chmod +x /usr/local/bin/eventsproc
```

### 2. Configure

Create `/etc/app/eventsproc/config.yaml`:

**PostgreSQL (default):**
```yaml
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
polling_interval: 60
```

**SQLite (NetBird's built-in store):**
```yaml
database_driver: sqlite
sqlite_path: /var/lib/netbird/store.db
polling_interval: 60
```

All other settings have sensible defaults. See `config.yaml.example` for full options.

### 3. Setup Database

Create the checkpoint table in your database. SQL files are in the `lab/` directory:

```bash
# PostgreSQL
psql -h postgres.example.com -U netbird -d netbird -f lab/init-db.sql

# SQLite
sqlite3 /var/lib/netbird/store.db < lab/init-sqlite.sql
```

### 4. Run Service

```bash
# One-time run (process events and exit)
eventsproc

# Continuous polling (run as service)
eventsproc --config /etc/app/eventsproc/config.yaml
```

### 5. Setup Log Forwarding (Optional)

To forward events to Loki/Splunk, configure OpenTelemetry Collector:

```bash
# See docs/SETUP.md for Grafana Alloy configuration
# Example: journald → loki + splunk
```

## Configuration

### Database

| Setting | Default | Description |
|---------|---------|-------------|
| `database_driver` | `postgres` | Backend to use: `postgres` or `sqlite` |
| `postgres_url` | — | PostgreSQL connection string. **Required** when `database_driver` is `postgres` |
| `sqlite_path` | `/var/lib/netbird/store.db` | Path to NetBird's SQLite file. Only used when `database_driver` is `sqlite` |

### General

| Setting | Default | Description |
|---------|---------|-------------|
| `platform` | `sandbox` | Environment label attached to events |
| `region` | `apac` | Region label attached to events |
| `consumer_id` | auto | Checkpoint identifier (auto-generated from platform+region) |
| `log_level` | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `batch_size` | `1000` | Events fetched per poll |
| `lookback_hours` | `24` | How far back to look on first run |
| `polling_interval` | `0` | Seconds between polls; `0` = run once and exit |
| `metrics_port` | `2113` | Port for Prometheus `/metrics` endpoint |

### Environment Variables

Override any config with `EP_` prefix:

```bash
# PostgreSQL mode
export EP_POSTGRES_URL="user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=db.example.com"

# SQLite mode
export EP_DATABASE_DRIVER=sqlite
export EP_SQLITE_PATH=/var/lib/netbird/store.db

export EP_PLATFORM="prod"
export EP_REGION="emea"
export EP_POLLING_INTERVAL=60
./eventsproc
```

### Email Enrichment

`eventsproc` can enrich events with user email addresses by joining with user tables. This is **disabled by default** because NetBird encrypts the `users` table at rest — without the decryption key, joining it yields ciphertext blobs, not readable email addresses.

**Configuration:**

```yaml
email_enrichment:
  enabled: false     # Default: false
  source: "none"     # Default: "none"
```

**Available Sources:**

| Source | Description | Use Case |
|--------|-------------|----------|
| `auto` | Try `users` table then fall back to `user_id` | Works for most NetBird deployments |
| `netbird_users` | Standard NetBird `users` table | Standard NetBird installations |
| `custom` | Custom schema/table | When you have your own user directory |
| `none` | No enrichment (show `user_id` only) | **Default** |

**Enable with decryption (standard NetBird):**

NetBird encrypts email and name fields using AES-256-GCM. Provide the key so eventsproc can decrypt them:

```yaml
email_enrichment:
  enabled: true
  source: "auto"
  # Option 1: provide the base64-encoded encryption key directly
  netbird_encryption_key: "BASE64_ENCODED_32_BYTE_KEY"
  # Option 2: point at NetBird's management.json (key is read automatically)
  netbird_config_path: "/etc/netbird/management.json"
```

**Custom Table Example:**

```yaml
email_enrichment:
  enabled: true
  source: "custom"
  custom_schema: "auth"
  custom_table: "user_directory"
```

Your custom table must have:
- `id` column (text/varchar) — matches NetBird `user_id`
- `email` column (text/varchar) — email address

**Environment Variables:**

```bash
export EP_EMAIL_ENRICHMENT_ENABLED=true
export EP_EMAIL_ENRICHMENT_SOURCE="auto"
export EP_EMAIL_ENRICHMENT_NETBIRD_ENCRYPTION_KEY="BASE64_ENCODED_KEY"
# or
export EP_EMAIL_ENRICHMENT_NETBIRD_CONFIG_PATH="/etc/netbird/management.json"
```

When enrichment is disabled, `initiator_email` and `target_email` contain the raw `user_id`.

## Building

```bash
make build  # Build binary
make test   # Run tests
make lint   # Lint code
make all    # Format + lint + test + build
```

**Requirements:**
- Go 1.21+
- No CGO required — SQLite driver is pure Go

## Deployment

### Systemd Service

```bash
# Install service file
sudo cp systemd/eventsproc.service /etc/systemd/system/
sudo systemctl daemon-reload

# Configure via environment
sudo mkdir -p /etc/app/eventsproc
sudo vi /etc/app/eventsproc/config.yaml

# Start service
sudo systemctl start eventsproc
sudo systemctl enable eventsproc

# View logs
journalctl -u eventsproc -f
```

### High Availability (HA)

For HA deployments, run multiple instances with unique `consumer_id`:

**Node 1:**
```bash
export EP_CONSUMER_ID="eventsproc-prod-node1"
export EP_POLLING_INTERVAL=60
./eventsproc
```

**Node 2:**
```bash
export EP_CONSUMER_ID="eventsproc-prod-node2"
export EP_POLLING_INTERVAL=60
./eventsproc
```

Each instance maintains its own checkpoint and processes events independently.

## Event Format

Events are output as JSON, one per line.

**With email enrichment enabled:**

```json
{
  "event_id": 12345,
  "timestamp": "2024-02-02T10:30:00Z",
  "activity": 1,
  "activity_code": "user.login",
  "activity_name": "User Login",
  "initiator_id": "00u123abc",
  "initiator_email": "user@example.com",
  "target_id": "00u456def",
  "target_email": "admin@example.com",
  "account_id": "acc-789",
  "meta": {"ip": "192.168.1.100"}
}
```

**With email enrichment disabled:**

```json
{
  "event_id": 12345,
  "timestamp": "2024-02-02T10:30:00Z",
  "activity": 1,
  "activity_code": "user.login",
  "activity_name": "User Login",
  "initiator_id": "00u123abc",
  "initiator_email": "00u123abc",
  "target_id": "00u456def",
  "target_email": "00u456def",
  "account_id": "acc-789",
  "meta": {"ip": "192.168.1.100"}
}
```

When email enrichment is disabled or no email is found, the `initiator_email` and `target_email` fields contain the `user_id` instead.

## Documentation

- **[Setup Guide](docs/SETUP.md)** - Installation, deployment, and log forwarding
- **[Technical Documentation](docs/TECH_DOC.md)** - Architecture and implementation details
- **[config.yaml.example](config.yaml.example)** - Full configuration reference
- **[Lab Guide](lab/README.md)** - Docker lab with simulated and real NetBird modes

## Database Schema

**PostgreSQL — required tables:**
- `events` — NetBird audit events (standard NetBird table)
- `idp.event_processing_checkpoint` — Processing state (created by `lab/init-db.sql`)

**SQLite — required tables:**
- `events` — NetBird's built-in event store
- `event_processing_checkpoint` — Processing state (created by `lab/init-sqlite.sql`)

**Optional (for email enrichment):**
- `users` — Standard NetBird users table (contains AES-GCM encrypted emails)
- Custom user table — Your own user directory (configure via `custom_schema` / `custom_table`)

See [Email Enrichment](#email-enrichment) for decryption setup.

## Monitoring

Check processing status:

```bash
# View service logs
journalctl -u eventsproc -n 100

# Check last processed event
psql -c "SELECT * FROM event_processing_checkpoint;"

# Verify events are flowing
journalctl -u eventsproc -o cat | grep event_id | tail -10
```

## Troubleshooting

**No events appearing:**
- Check database connectivity: `psql -h <host> -U <user> -d <dbname> -c "SELECT COUNT(*) FROM events;"`
- Verify checkpoint: `SELECT * FROM event_processing_checkpoint WHERE consumer_id = 'your-consumer-id';`
- Check service logs: `journalctl -u eventsproc -f`

**Duplicate events:**
- Ensure each instance has a unique `consumer_id`
- Check that only one instance is running (unless HA setup)

**Events not reaching Loki/Splunk:**
- Verify OTEL Collector is running: `systemctl status alloy`
- Check Alloy logs: `journalctl -u alloy -f`
- Test Alloy configuration: `alloy run --config /etc/alloy/config.alloy`

## License

MIT License - See [LICENSE](LICENSE) file

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

---

**Questions?** Open an issue at https://github.com/xh63/netbird-events/issues
