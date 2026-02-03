# Setup Guide

Complete installation and configuration guide for NetBird Events Exporter.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Database Setup](#database-setup)
- [Log Forwarding Setup](#log-forwarding-setup)
- [Production Deployment](#production-deployment)
- [High Availability](#high-availability)

## Prerequisites

### Required
- **PostgreSQL 12+** with NetBird schema
- **Go 1.21+** (for building from source)
- **systemd** (for running as a service)

### Optional (for log forwarding)
- **Grafana Alloy** or **OpenTelemetry Collector**
- **Loki** and/or **Splunk** endpoints

## Installation

### Option 1: Build from Source

```bash
# Clone repository
git clone https://github.com/xh63/netbird-events.git
cd netbird-events

# Build binary
make build

# Install
sudo cp bin/eventsproc /usr/local/bin/
sudo chmod +x /usr/local/bin/eventsproc

# Verify installation
eventsproc version
```

### Option 2: Download Binary

```bash
# Download latest release
curl -LO https://github.com/xh63/netbird-events/releases/latest/download/eventsproc-linux-amd64

# Install
sudo mv eventsproc-linux-amd64 /usr/local/bin/eventsproc
sudo chmod +x /usr/local/bin/eventsproc
```

## Configuration

### 1. Create Config Directory

```bash
sudo mkdir -p /etc/app/eventsproc
sudo mkdir -p /etc/app/eventsproc/ssl  # For SSL certificates if needed
```

### 2. Create Configuration File

Create `/etc/app/eventsproc/config.yaml`:

**Minimal Configuration (Single Instance):**
```yaml
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com sslmode=require"
polling_interval: 60
```

**Production Configuration:**
```yaml
# Database connection (REQUIRED)
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com sslmode=require"

# Environment metadata (OPTIONAL)
platform: "prod"
region: "emea"

# Processing options (OPTIONAL)
log_level: "info"
batch_size: 1000
lookback_hours: 24
polling_interval: 60
```

**HA Configuration (Multiple Nodes):**
```yaml
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com sslmode=require"
platform: "prod"
region: "emea"
consumer_id: "eventsproc-node1"  # Unique per node
polling_interval: 60
```

### 3. Secure Configuration File

```bash
sudo chmod 600 /etc/app/eventsproc/config.yaml
sudo chown root:root /etc/app/eventsproc/config.yaml
```

### 4. Environment Variables (Recommended for Secrets)

For production, use environment variables for sensitive data:

```bash
# /etc/default/eventsproc or systemd override
export EP_POSTGRES_URL="user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
export EP_PLATFORM="prod"
export EP_REGION="emea"
export EP_LOG_LEVEL="info"
export EP_POLLING_INTERVAL=60
```

## Database Setup

### 1. Create Checkpoint Table

```bash
# Download migration
wget https://raw.githubusercontent.com/xh63/netbird-events/main/migrations/001_create_checkpoint_table.sql

# Apply migration
psql -h postgres.example.com -U netbird -d netbird -f 001_create_checkpoint_table.sql
```

**Or manually:**
```sql
CREATE TABLE IF NOT EXISTS event_processing_checkpoint (
    consumer_id VARCHAR(255) PRIMARY KEY,
    last_event_id BIGINT NOT NULL,
    last_event_timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    total_events_processed BIGINT DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_checkpoint_consumer ON event_processing_checkpoint(consumer_id);
```

### 2. Verify Database Access

```bash
psql -h postgres.example.com -U netbird -d netbird -c "SELECT COUNT(*) FROM events;"
```

### 3. Optional: Create Read-Only User

```sql
-- Create read-only user for eventsproc
CREATE USER eventsproc_reader WITH PASSWORD 'secure_password';

-- Grant permissions
GRANT CONNECT ON DATABASE netbird TO eventsproc_reader;
GRANT USAGE ON SCHEMA public TO eventsproc_reader;
GRANT SELECT ON events TO eventsproc_reader;
GRANT SELECT ON okta_users TO eventsproc_reader;
GRANT SELECT, INSERT, UPDATE ON event_processing_checkpoint TO eventsproc_reader;
```

## Log Forwarding Setup

The service outputs JSON to stdout. To forward logs to Loki/Splunk, configure Grafana Alloy or OTEL Collector.

### Grafana Alloy Setup

See **[ALLOY_SETUP.md](../ALLOY_SETUP.md)** for complete configuration.

**Quick Example:**

1. Install Grafana Alloy:
```bash
curl -LO https://github.com/grafana/alloy/releases/latest/download/alloy-linux-amd64
sudo mv alloy-linux-amd64 /usr/local/bin/alloy
sudo chmod +x /usr/local/bin/alloy
```

2. Create `/etc/alloy/config.alloy`:
```hcl
// Read from systemd journal
loki.source.journal "eventsproc" {
  path = "/var/log/journal"
  matches = "_SYSTEMD_UNIT=eventsproc.service"
  forward_to = [loki.write.local.receiver]
}

// Write to Loki
loki.write "local" {
  endpoint {
    url = "https://loki.example.com:3100/loki/api/v1/push"
  }
}
```

3. Start Alloy:
```bash
sudo systemctl start alloy
sudo systemctl enable alloy
```

## Production Deployment

### 1. Install Systemd Service

```bash
# Copy service file
sudo cp systemd/eventsproc.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload
```

### 2. Configure Service

Edit `/etc/systemd/system/eventsproc.service` if needed, or create an override:

```bash
sudo systemctl edit eventsproc
```

Add environment variables:
```ini
[Service]
Environment="EP_POSTGRES_URL=user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
Environment="EP_PLATFORM=prod"
Environment="EP_REGION=emea"
Environment="EP_POLLING_INTERVAL=60"
```

### 3. Start Service

```bash
# Start service
sudo systemctl start eventsproc

# Enable on boot
sudo systemctl enable eventsproc

# Check status
sudo systemctl status eventsproc

# View logs
journalctl -u eventsproc -f
```

### 4. Verify Events

```bash
# Check logs are flowing
journalctl -u eventsproc -o cat | grep event_id | tail -20

# Check checkpoint
psql -h postgres.example.com -U netbird -d netbird -c \
  "SELECT * FROM event_processing_checkpoint;"
```

## High Availability

For HA deployments, run multiple instances with unique `consumer_id` values.

### Setup Node 1

```bash
# /etc/systemd/system/eventsproc.service.d/override.conf
[Service]
Environment="EP_CONSUMER_ID=eventsproc-prod-emea-node1"
Environment="EP_POSTGRES_URL=user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
Environment="EP_PLATFORM=prod"
Environment="EP_REGION=emea"
Environment="EP_POLLING_INTERVAL=60"

# Start
sudo systemctl start eventsproc
```

### Setup Node 2

```bash
# /etc/systemd/system/eventsproc.service.d/override.conf
[Service]
Environment="EP_CONSUMER_ID=eventsproc-prod-emea-node2"
Environment="EP_POSTGRES_URL=user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com"
Environment="EP_PLATFORM=prod"
Environment="EP_REGION=emea"
Environment="EP_POLLING_INTERVAL=60"

# Start
sudo systemctl start eventsproc
```

### Verify HA Setup

```bash
# Check both checkpoints exist
psql -h postgres.example.com -U netbird -d netbird -c \
  "SELECT consumer_id, last_event_id, updated_at FROM event_processing_checkpoint ORDER BY consumer_id;"

# Should show both node1 and node2
```

**HA Behavior:**
- Each instance maintains its own checkpoint
- Both process all events independently
- Safe for active-active or active-passive setups
- No coordination needed between nodes

## Monitoring

### Service Health

```bash
# Service status
sudo systemctl status eventsproc

# Recent logs
journalctl -u eventsproc -n 100

# Follow logs
journalctl -u eventsproc -f

# Errors only
journalctl -u eventsproc -p err
```

### Processing Status

```bash
# Check last processed event
psql -c "SELECT * FROM event_processing_checkpoint WHERE consumer_id = 'your-consumer-id';"

# Count events in database
psql -c "SELECT COUNT(*) FROM events;"

# Count events processed today
psql -c "SELECT COUNT(*) FROM events WHERE timestamp::date = CURRENT_DATE;"
```

### Verify Log Forwarding

```bash
# Check Alloy is running
sudo systemctl status alloy

# Check Alloy logs
journalctl -u alloy -f

# Test Loki query
curl -G "https://loki.example.com:3100/loki/api/v1/query" \
  --data-urlencode 'query={service_name="eventsproc"}' | jq
```

## Troubleshooting

### Service Won't Start

```bash
# Check configuration syntax
eventsproc --config /etc/app/eventsproc/config.yaml --help

# Check database connectivity
psql -h postgres.example.com -U netbird -d netbird -c "SELECT 1;"

# Check permissions
ls -la /etc/app/eventsproc/
```

### No Events Processing

```bash
# Check if events exist in database
psql -c "SELECT COUNT(*) FROM events;"

# Check checkpoint status
psql -c "SELECT * FROM event_processing_checkpoint;"

# Reset checkpoint (CAUTION: will reprocess events)
psql -c "DELETE FROM event_processing_checkpoint WHERE consumer_id = 'your-consumer-id';"
```

### Events Not Reaching Loki/Splunk

```bash
# Verify events are in journal
journalctl -u eventsproc -o cat | grep event_id

# Check Alloy status
systemctl status alloy

# Check Alloy config
alloy validate /etc/alloy/config.alloy

# Test Loki endpoint
curl -v https://loki.example.com:3100/ready
```

## Security Best Practices

1. **Use SSL for database connections** (`sslmode=require`)
2. **Store passwords in environment variables**, not config files
3. **Use read-only database user** for eventsproc
4. **Restrict config file permissions** (`chmod 600`)
5. **Use mTLS for Loki/Splunk** if available
6. **Run service as non-root user**
7. **Enable audit logging** on database

## Next Steps

- Configure log forwarding to Loki/Splunk (see [ALLOY_SETUP.md](../ALLOY_SETUP.md))
- Set up monitoring and alerting
- Configure backup for checkpoint table
- Test failover in HA setup

---

**Need help?** Open an issue at https://github.com/xh63/netbird-events/issues
