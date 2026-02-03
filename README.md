# NetBird Events Exporter

Export NetBird audit events from PostgreSQL to centralized logging platforms (Loki, Splunk) via OpenTelemetry Collector.

## Overview

`eventsproc` is a lightweight Go service that:
- ✅ Reads events from NetBird PostgreSQL database
- ✅ Enriches events with user emails and activity names (configurable)
- ✅ Outputs structured JSON to stdout
- ✅ Forwards to Loki/Splunk via OpenTelemetry Collector
- ✅ Maintains checkpoints for reliable processing
- ✅ Supports High Availability (HA) deployments
- ✅ Works with both standard NetBird and custom deployments

## Architecture

```
PostgreSQL → eventsproc → stdout → systemd journal → OTEL Collector → Loki/Splunk
  (events)    (enriches)  (JSON)   (captures)        (forwards)       (stores)
```

**Key Design:**
- `eventsproc` outputs JSON to stdout only
- systemd captures logs to journal
- Grafana Alloy/OTEL Collector reads from journal and forwards to destinations
- Clean separation of concerns: processing vs forwarding

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

```yaml
# Minimal configuration - only postgres_url is required
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
polling_interval: 60  # Poll every 60 seconds
```

All other settings have sensible defaults. See `config.yaml.example` for full options.

### 3. Setup Database

```bash
# Create checkpoint table
psql -h postgres.example.com -U netbird -d netbird -f migrations/001_create_checkpoint_table.sql
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

### Required
- `postgres_url` - Database connection string (or use `EP_POSTGRES_URL` env var)

### Optional
- `platform` - Environment label (default: "sandbox")
- `region` - Region label (default: "apac")
- `consumer_id` - Checkpoint ID (default: auto-generated from platform+region)
- `log_level` - Logging level (default: "info")
- `batch_size` - Events per batch (default: 1000)
- `lookback_hours` - Initial lookback on first run (default: 24)
- `polling_interval` - Seconds between polls, 0=run once (default: 0)

### Environment Variables

Override any config with `EP_` prefix:

```bash
export EP_POSTGRES_URL="user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=db.example.com"
export EP_PLATFORM="prod"
export EP_REGION="emea"
export EP_POLLING_INTERVAL=60
./eventsproc
```

### Email Enrichment

`eventsproc` can enrich events with user email addresses by joining with user tables. This is **optional** and highly configurable.

**Background:**
NetBird stores `user_id` (from Okta/OIDC) in events but not email addresses. The email enrichment feature looks up emails from various sources.

**Configuration:**

```yaml
email_enrichment:
  enabled: true      # Default: true
  source: "auto"     # Default: "auto"
```

**Available Sources:**

| Source | Description | Use Case |
|--------|-------------|----------|
| `auto` | Try multiple sources with fallback | **Recommended** - Works everywhere |
| `ais_okta_users` | Company's custom `okta_users` table | Company deployments only |
| `netbird_users` | Standard NetBird `users` table | Standard NetBird installations |
| `custom` | Custom schema/table | When you have your own user directory |
| `none` | Disable (show `user_id` only) | When emails not needed |

**Auto Mode (Default):**

Tries sources in order with graceful fallback:
1. `okta_users` table (if exists)
2. `users` table (standard NetBird)
3. `user_id` (if no email found)

This works for both Company deployments and standard NetBird installations.

**Custom Table Example:**

```yaml
email_enrichment:
  enabled: true
  source: "custom"
  custom_schema: "auth"
  custom_table: "user_directory"
```

Your custom table must have:
- `id` column (text/varchar) - matches NetBird `user_id`
- `email` column (text/varchar) - email address

**Environment Variables:**

```bash
export EP_EMAIL_ENRICHMENT_ENABLED=true
export EP_EMAIL_ENRICHMENT_SOURCE="auto"
export EP_EMAIL_ENRICHMENT_CUSTOM_SCHEMA="auth"
export EP_EMAIL_ENRICHMENT_CUSTOM_TABLE="users"
```

**Disable Email Enrichment:**

```yaml
email_enrichment:
  enabled: false
```

Or:

```bash
export EP_EMAIL_ENRICHMENT_ENABLED=false
```

When disabled, events show `user_id` in `initiator_email` and `target_email` fields.

**See Also:**
- Full configuration details: `config.yaml.example`
- Troubleshooting: Search for "EMAIL ENRICHMENT" in `config.yaml.example`

## Building

```bash
make build  # Build binary
make test   # Run tests
make lint   # Lint code
make all    # Format + lint + test + build
```

**Requirements:**
- Go 1.21+
- PostgreSQL client libraries

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

**With email enrichment enabled (default):**

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
- **[Workflow Guide](docs/WORKFLOW.md)** - Processing workflow and operational guide
- **[config.yaml.example](config.yaml.example)** - Full configuration reference
- **[ALLOY_SETUP.md](ALLOY_SETUP.md)** - Grafana Alloy configuration for log forwarding

## Database Schema

**Required Tables:**
- `events` - NetBird audit events (standard NetBird table)
- `idp.event_processing_checkpoint` - Processing state (created by migration scripts)

**Optional Tables (for email enrichment):**
- `okta_users` - Company's Okta user cache (Company deployments only)
- `users` - Standard NetBird users table (may have email field)
- Custom user table - Your own user directory (specify via `custom_schema.custom_table`)

Email enrichment automatically tries available sources based on your configuration. See [Email Enrichment](#email-enrichment) section.

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
