# Technical Design Document: eventsproc

**Project:** NetBird Events Processor (eventsproc)
**Version:** 2.0.0
**Date:** January 2026
**Author:** Company VPN Infrastructure Team
**Status:** Production Ready
**Classification:** Internal Use Only

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [System Overview](#2-system-overview)
3. [Architecture](#3-architecture)
4. [Security Design](#4-security-design)
5. [Data Flow](#5-data-flow)
6. [Configuration Reference](#6-configuration-reference)
7. [Deployment Guide](#7-deployment-guide)
8. [Operations Manual](#8-operations-manual)
9. [Testing Strategy](#9-testing-strategy)
10. [Dependencies](#10-dependencies)
11. [Risk Assessment](#11-risk-assessment)
12. [Appendices](#appendices)

---

## 1. Executive Summary

### 1.1 Purpose

eventsproc is a Go-based service that reads NetBird events from a PostgreSQL database and outputs them as structured JSON to stdout. The output is captured by systemd journal, where an OpenTelemetry Collector scrapes and forwards events to multiple destinations (Loki, Splunk, etc.).

### 1.2 Key Features

| Feature | Description |
|---------|-------------|
| **Stdout-Only Output** | Single, simple output model - JSON to stdout/journal |
| **Decoupled Architecture** | No direct integration with Loki/Splunk - uses OTEL Collector |
| **Single Checkpoint** | One checkpoint per consumer for simplified progress tracking |
| **High Availability** | Database checkpoint system enables seamless failover |
| **Email Enrichment** | Okta user ID to email resolution via database join |
| **Graceful Shutdown** | Signal handling for clean process termination |
| **Metadata Flattening** | Nested JSON metadata flattened for easier querying |

### 1.3 Target Environment

- **Platform:** Linux (systemd-based distributions)
- **Deployment Model:** Active/passive with keepalived
- **Regions:** EMEA (eu), APAC (au)
- **Environments:** sandbox, preprod, prod

### 1.4 Architecture Evolution

**v2.0.0 (Current):**
- Single stdout writer outputting JSON
- Single checkpoint per consumer
- OpenTelemetry Collector for log forwarding
- ~600 lines of code removed (Loki/Splunk integrations)

**v1.x (Legacy):**
- Direct Loki and Splunk integrations
- Per-writer checkpoints (3 per consumer)
- Credentials managed in eventsproc config

---

## 2. System Overview

### 2.1 Problem Statement

NetBird stores user and peer activity events in a PostgreSQL database. These events need to be:
1. Forwarded to centralized logging (Loki/Grafana) for operational visibility
2. Sent to SIEM (Splunk) for security monitoring and compliance
3. Available locally (systemd journal) for debugging

### 2.2 Solution

eventsproc provides a single, reliable service that:
- Polls the events table at configurable intervals
- Enriches events with Okta user email addresses
- Outputs events as JSON to stdout (captured by systemd journal)
- Maintains a single processing checkpoint for exactly-once delivery semantics
- Supports high availability through database-persisted state

An OpenTelemetry Collector:
- Scrapes audit events from systemd journal
- Filters out system logs (keeps only events with `event_id` field)
- Forwards to Loki, Splunk, and other destinations
- Provides observability metrics

### 2.3 System Context Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                eventsproc                                    │
│                                                                              │
│  ┌─────────────┐       ┌──────────────┐       ┌──────────────────────┐     │
│  │ PostgreSQL  │──────▶│  eventsproc  │──────▶│ stdout (JSON output) │     │
│  │   events    │       │              │       └──────────┬───────────┘     │
│  └─────────────┘       └──────────────┘                  │                 │
│                                                           │                 │
└───────────────────────────────────────────────────────────┼─────────────────┘
                                                            │
                                                            ▼
                                                ┌───────────────────────┐
                                                │   systemd journal     │
                                                │  (event storage)      │
                                                └───────────┬───────────┘
                                                            │
                                                            ▼
                                            ┌───────────────────────────────┐
                                            │   OpenTelemetry Collector     │
                                            │                               │
                                            │  • journald receiver          │
                                            │  • JSON parser                │
                                            │  • Filter audit events        │
                                            │  • Batch processor            │
                                            │  • Metrics exposure           │
                                            └───────┬───────────────────────┘
                                                    │
                                     ┌──────────────┼──────────────┐
                                     │              │              │
                                     ▼              ▼              ▼
                          ┌──────────────┐  ┌──────────┐  ┌──────────────┐
                          │   Loki       │  │  Splunk  │  │   Other      │
                          │ (Grafana)    │  │  (SIEM)  │  │ Destinations │
                          └──────────────┘  └──────────┘  └──────────────┘
```

### 2.4 Benefits of OTEL Collector Architecture

| Benefit | Description |
|---------|-------------|
| **Simpler Code** | ~600 lines removed (no Loki/Splunk direct integration) |
| **Better Security** | Loki/Splunk credentials only in OTEL config, not eventsproc |
| **More Flexible** | Add new destinations by reconfiguring OTEL only (no code changes) |
| **Better Observability** | OTEL provides metrics on forwarding (drop rate, latency) |
| **Easier Testing** | Test eventsproc locally without Loki/Splunk access |
| **Single Checkpoint** | Simpler database model, one checkpoint per consumer |

---

## 3. Architecture

### 3.1 Component Diagram

```
┌────────────────────────────────────────────────────────────────────────────┐
│                              eventsproc                                     │
│                                                                             │
│  ┌─────────────┐     ┌───────────────────────────────────────────────┐     │
│  │    main     │────▶│               Processor                        │     │
│  │   (cmd/)    │     │            (orchestrator)                      │     │
│  └─────────────┘     │                                                │     │
│        │             │  ┌──────────────────────────────────────────┐  │     │
│        │             │  │        Stdout Writer                     │  │     │
│        │             │  │  (JSON output to stdout)                 │  │     │
│        │             │  │                                          │  │     │
│        │             │  │  • Formats events as JSON                │  │     │
│        │             │  │  • Flattens meta fields                  │  │     │
│        │             │  │  • Writes to stdout                      │  │     │
│        │             │  │                                          │  │     │
│        │             │  │  ┌────────────────────────────┐          │  │     │
│        │             │  │  │  Single Checkpoint         │          │  │     │
│        │             │  │  │  (per consumer_id)         │          │  │     │
│        │             │  │  └────────────────────────────┘          │  │     │
│        │             │  └──────────────────────────────────────────┘  │     │
│        │             └───────────────────────────────────────────────┘     │
│        │                                  │                                │
│        ▼                                  ▼                                │
│  ┌─────────────┐                ┌──────────────────┐                       │
│  │   Config    │                │   EventReader    │                       │
│  │  (YAML+ENV) │                │   (database)     │                       │
│  └─────────────┘                └──────────────────┘                       │
└────────────────────────────────────────────────────────────────────────────┘

Simplified architecture: Single writer, single checkpoint.
OTEL Collector handles distribution to multiple destinations.
```

### 3.2 Package Structure

| Package | Path | Description |
|---------|------|-------------|
| `main` | `eventsproc/cmd/` | CLI entrypoint, signal handling |
| `config` | `eventsproc/pkg/config/` | Configuration loading (Viper) |
| `processor` | `eventsproc/pkg/processor/` | Main orchestration, single writer processing |
| `activity` | `eventsproc/pkg/activity/` | Activity code mappings |
| `events` | `pkg/events/` | Event types, database reader, single checkpoint |
| `stdout` | `pkg/stdout/` | JSON writer to stdout |

**Removed packages** (v2.0.0):
- `pkg/loki/` - Loki HTTP client (replaced by OTEL Collector)
- `pkg/splunk/` - Splunk HEC client (replaced by OTEL Collector)

### 3.3 Key Design Patterns

#### 3.3.1 Single Writer Pattern

eventsproc uses a single writer that outputs JSON to stdout:

```go
type Processor struct {
    eventReader *events.EventReader
    writer      EventWriter                    // Single stdout writer
    checkpoint  *events.ProcessingCheckpoint   // Single checkpoint
    config      *config.Config
    logger      *slog.Logger
    hostname    string // for processing_node tracking
}

func (p *Processor) processEvents(ctx context.Context) error {
    // Process events with single writer
    eventBatch, err := p.eventReader.GetEvents(ctx, opts)
    if err != nil {
        return fmt.Errorf("failed to fetch events: %w", err)
    }

    // Send to stdout writer
    if err := p.writer.SendEvents(ctx, eventBatch); err != nil {
        return fmt.Errorf("failed to send events: %w", err)
    }

    // Update single checkpoint
    p.checkpoint.LastEventID = lastEvent.ID
    if err := p.eventReader.SaveCheckpoint(ctx, p.checkpoint); err != nil {
        return fmt.Errorf("failed to save checkpoint: %w", err)
    }

    return nil
}
```

**Design Decision:** Single writer simplifies the architecture. Distribution to multiple destinations is handled by OpenTelemetry Collector.

#### 3.3.2 Single Checkpoint Pattern

Database-persisted checkpoint enables:
- Exactly-once delivery semantics
- Seamless failover between HA nodes
- Resume from last processed event after restart

```sql
CREATE TABLE event_processing_checkpoint (
    consumer_id VARCHAR(255) PRIMARY KEY,
    last_event_id BIGINT NOT NULL DEFAULT 0,
    last_event_timestamp TIMESTAMP WITH TIME ZONE,
    total_events_processed BIGINT NOT NULL DEFAULT 0,
    processing_node VARCHAR(255),               -- hostname for HA tracking
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
```

**Checkpoint Fields:**
- `consumer_id`: Logical consumer group (e.g., `eventsproc-sandbox-apac`)
- `processing_node`: Hostname of node that last updated (for HA debugging)
- `last_event_id`: Last successfully processed event ID

**Migration from Per-Writer Checkpoints:**

Version 1.x used per-writer checkpoints with a composite primary key `(consumer_id, writer_type)`. Migration 003 simplified this:
1. Kept the checkpoint with highest `last_event_id` (usually from journal writer)
2. Removed `writer_type` column
3. Kept `processing_node` column for HA tracking
4. Changed primary key to just `consumer_id`

#### 3.3.3 JSON Metadata Flattening

The stdout writer flattens nested `meta` JSON for easier querying:

```go
// Original event.Meta: {"fqdn":"host.example.com","ip":"10.0.0.1","os":"linux"}

// Output JSON:
{
  "event_id": 123,
  "meta_fqdn": "host.example.com",
  "meta_ip": "10.0.0.1",
  "meta_os": "linux"
}
```

This allows querying specific metadata fields in Loki/Splunk without JSON parsing.

---

## 4. Security Design

### 4.1 Authentication & Authorization

| Component | Mechanism | Details |
|-----------|-----------|---------|
| PostgreSQL | Username/Password | Connection string in config/env |
| Stdout | System | Writes to stdout; permissions handled by service manager |
| OTEL Collector | mTLS/Tokens | Loki/Splunk credentials in OTEL config (not eventsproc) |

**Security Improvement:** eventsproc no longer needs Loki/Splunk credentials. All destination credentials are managed in the OpenTelemetry Collector configuration.

### 4.2 Transport Security

eventsproc outputs to stdout, which is captured by systemd journal. The OpenTelemetry Collector handles TLS connections to destinations.

**OTEL Collector TLS Configuration:**
- Loki: mTLS with client certificates
- Splunk: HTTPS with HEC token + optional mTLS

See [OTEL_COLLECTOR_SETUP.md](OTEL_COLLECTOR_SETUP.md) for detailed TLS configuration.

### 4.3 Secrets Management

| Secret | Storage | Recommendation |
|--------|---------|----------------|
| `postgres_url` | Config/Environment | Use `EP_POSTGRES_URL` env var |
| Loki/Splunk credentials | OTEL Collector config | Manage in OTEL config files, use environment variables |

### 4.4 Data Classification

| Data Type | Classification | Handling |
|-----------|---------------|----------|
| Event timestamps | Internal | Logged, transmitted |
| User IDs (Okta) | Internal | Logged, transmitted |
| Email addresses | PII | Logged, transmitted (enrichment) |
| Activity metadata | Internal | May contain IPs, hostnames |

---

## 5. Data Flow

### 5.1 Event Processing Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Event Processing Flow                               │
└─────────────────────────────────────────────────────────────────────────────┘

    ┌─────────────┐
    │   Start     │
    └──────┬──────┘
           │
           ▼
    ┌──────────────────────────────┐
    │ Load checkpoint from DB      │
    │ (consumer_id)                │
    └──────────┬───────────────────┘
               │
               ▼
    ┌──────────────────────┐      ┌─────────────────────────┐
    │  Checkpoint found?   │──No──▶│ Use lookback_hours or   │
    └──────────┬───────────┘      │ process all events      │
               │                   └────────────┬────────────┘
              Yes                               │
               │                                │
               ▼                                │
    ┌──────────────────────┐                   │
    │ Resume from          │◀──────────────────┘
    │ last_id + 1          │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │ Fetch batch of       │
    │ events from DB       │
    └──────────┬───────────┘
               │
               ▼
    ┌──────────────────────┐
    │   Batch empty?       │──Yes──▶ Done (wait for polling_interval or exit)
    └──────────┬───────────┘
               │
              No
               │
               ▼
    ┌──────────────────────┐
    │ Send to stdout as    │──Error──▶ Return error, checkpoint not updated
    │ JSON                 │           (will retry on next run)
    └──────────┬───────────┘
               │
             Success
               │
               ▼
    ┌──────────────────────┐
    │ Update checkpoint    │
    │ in DB                │
    └──────────┬───────────┘
               │
               ▼
         Loop to "Fetch batch"
```

**Key Points:**
- Single writer, single checkpoint
- Checkpoint updated only after successful output to stdout
- Failed batch causes retry on next poll (no checkpoint update)
- OTEL Collector scrapes from journal asynchronously

### 5.2 Event Schema

#### 5.2.1 Database Schema (public.events)

```sql
SELECT
    e.id,
    e.timestamp,
    e.activity,
    e.initiator_id,
    e.target_id,
    e.account_id,
    e.meta,
    COALESCE(u1.email, 'not_okta_user') AS initiator_email,
    COALESCE(u2.email, 'not_okta_user') AS target_email
FROM events e
LEFT JOIN okta_users u1 ON e.initiator_id = u1.id
LEFT JOIN okta_users u2 ON e.target_id = u2.id
WHERE e.id > $1
ORDER BY e.id ASC
LIMIT $2;
```

#### 5.2.2 Output JSON Schema

```json
{
  "event_id": 49134,
  "timestamp": "2025-11-12T07:05:12.553Z",
  "activity": 49,
  "activity_name": "Peer login expired",
  "activity_code": "peer.login.expire",
  "initiator_id": "00ugmqdg523psDShk0i7",
  "initiator_email": "john.doe@example.com",
  "target_id": "d3of8ropndkpb37pjbf0",
  "target_email": "jane.smith@example.com",
  "account_id": "cu86ogfndlrpb32kieh0",
  "service_name": "eventsproc",
  "meta_fqdn": "wl-gkvffl3.netbird.giservices",
  "meta_ip": "100.89.28.126",
  "meta_created_at": "2025-10-16T13:34:39.180594Z"
}
```

**Note:** The original `meta` field (nested JSON) is flattened into individual `meta_*` fields.

### 5.3 OpenTelemetry Collector Flow

```
systemd journal (MESSAGE field contains JSON)
    ↓
OTEL journald receiver
    ↓
JSON parser (parse from body.MESSAGE)
    ↓
Filter operator (keep only logs with event_id field)
    ↓
Batch processor
    ↓
    ├─▶ Loki exporter (with service_name label)
    └─▶ Splunk HEC exporter (with event structure)
```

See [OTEL_COLLECTOR_SETUP.md](OTEL_COLLECTOR_SETUP.md) for configuration details.

### 5.4 Timestamp Handling

**Critical Design Decision:** The `timestamp` field is the **original database timestamp**, not the ingestion time.

| Target | Timestamp Source | Implementation |
|--------|-----------------|----------------|
| Stdout | `event.Timestamp` | Included in JSON output |
| OTEL Collector | Parsed from JSON | Uses timestamp from JSON field |
| Loki | `event.Timestamp` | OTEL forwards original timestamp |
| Splunk | `event.Timestamp` | OTEL converts to Unix timestamp |

**Benefits:**
- Accurate historical timeline in Grafana/Splunk
- Correct event correlation with other systems
- Safe backfilling of historical events without timestamp confusion
- Query by actual event time, not processing time

---

## 6. Configuration Reference

### 6.1 Configuration File

Default location: `/etc/app/eventsproc/config.yaml`

```yaml
# Database connection (required)
postgres_url: "user=netbird password=YOUR_PASSWORD_HERE dbname=netbird host=postgres.example.com sslmode=disable"

# Environment identification
platform: "sandbox"  # sandbox, preprod, prod
region: "emea"       # emea, apac

# Consumer ID (optional - auto-generated as eventsproc-{platform}-{region})
consumer_id: ""

# Processing options
log_level: "info"        # debug, info, warn, error
batch_size: 1000         # Events per batch
lookback_hours: 24       # Initial lookback (0 = all)
polling_interval: 30     # Seconds between polls (0 = run once)
```

**Removed configuration** (v2.0.0):
- `output_targets` - Now always outputs to stdout
- `loki_*` - Loki config moved to OTEL Collector
- `splunk_*` - Splunk config moved to OTEL Collector
- `tenant` - No longer needed with single output

### 6.2 Environment Variables

All configuration can be overridden with `EP_` prefix:

| Environment Variable | Config Key | Example |
|---------------------|------------|---------|
| `EP_POSTGRES_URL` | `postgres_url` | `user=netbird password=YOUR_PASSWORD_HERE |
| `EP_PLATFORM` | `platform` | `sandbox` |
| `EP_REGION` | `region` | `emea` |
| `EP_CONSUMER_ID` | `consumer_id` | `eventsproc-custom-id` |
| `EP_LOG_LEVEL` | `log_level` | `debug` |
| `EP_BATCH_SIZE` | `batch_size` | `500` |
| `EP_LOOKBACK_HOURS` | `lookback_hours` | `48` |
| `EP_POLLING_INTERVAL` | `polling_interval` | `60` |

### 6.3 CLI Options

```bash
eventsproc [options]

Options:
  --config string    Path to configuration file (default: /etc/app/eventsproc/config.yaml)
  --version          Show version and exit
```

---

## 7. Deployment Guide

### 7.1 Prerequisites

- Linux with systemd
- Go 1.21+ (for building)
- PostgreSQL access (read/write to events, checkpoint tables)
- OpenTelemetry Collector installed (for log forwarding)

### 7.2 Database Migration

**Required before first run:**

#### New Deployments

```bash
# Create checkpoint table
psql -h <postgres-host> -U netbird -d netbird \
  -f eventsproc/migrations/001_create_checkpoint_table.sql

# Apply single checkpoint model
psql -h <postgres-host> -U netbird -d netbird \
  -f eventsproc/migrations/003_revert_to_single_checkpoint.sql
```

#### Existing Deployments (Upgrading from Multi-Writer v1.x)

```bash
# Stop eventsproc on all nodes first!
systemctl stop eventsproc

# Migrate to single checkpoint
psql -h <postgres-host> -U netbird -d netbird \
  -f eventsproc/migrations/003_revert_to_single_checkpoint.sql
```

Verify:
```sql
\d event_processing_checkpoint

-- Should show columns:
-- consumer_id (PK)
-- last_event_id
-- last_event_timestamp
-- total_events_processed
-- processing_node
-- updated_at
-- created_at

SELECT * FROM event_processing_checkpoint ORDER BY consumer_id;
```

### 7.3 Build

```bash
cd eventsproc
make build
# Output: bin/eventsproc
```

For all platforms:
```bash
make all  # fmt → lint → test → build
```

### 7.4 Installation

```bash
# Copy binary
sudo cp bin/eventsproc /usr/local/bin/
sudo chmod +x /usr/local/bin/eventsproc

# Create config directory
sudo mkdir -p /etc/app/eventsproc
sudo chown netbird:netbird /etc/app/eventsproc
sudo chmod 750 /etc/app/eventsproc

# Install configuration
sudo cp config.yaml.example /etc/app/eventsproc/config.yaml
sudo chmod 640 /etc/app/eventsproc/config.yaml
sudo chown netbird:netbird /etc/app/eventsproc/config.yaml
```

### 7.5 Systemd Service

Create `/etc/systemd/system/eventsproc.service`:

```ini
[Unit]
Description=NetBird Events Processor
After=network.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=netbird
Group=netbird
ExecStart=/usr/local/bin/eventsproc --config=/etc/app/eventsproc/config.yaml
Restart=on-failure
RestartSec=30s
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

# Environment file for secrets
EnvironmentFile=-/etc/sysconfig/eventsproc

[Install]
WantedBy=multi-user.target
```

**Important:** `StandardOutput=journal` ensures JSON output goes to systemd journal for OTEL Collector to scrape.

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable eventsproc
sudo systemctl start eventsproc
sudo systemctl status eventsproc
```

### 7.6 OpenTelemetry Collector Setup

See [OTEL_COLLECTOR_SETUP.md](OTEL_COLLECTOR_SETUP.md) for complete setup instructions including:
- OTEL Collector installation
- Configuration for journald receiver
- Loki and Splunk exporters
- TLS/mTLS setup
- Filtering audit events from system logs

Quick checklist:
1. Install otelcol-contrib
2. Configure journald receiver for eventsproc.service
3. Add JSON parser and filter for audit events
4. Configure Loki and Splunk exporters
5. Start otelcol-eventsproc.service

### 7.7 High Availability with Keepalived

#### 7.7.1 Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                 3-Node Keepalived Cluster                        │
│                                                                  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │    Node 1       │  │    Node 2       │  │    Node 3       │  │
│  │    (MASTER)     │  │   (BACKUP)      │  │   (BACKUP)      │  │
│  │                 │  │                 │  │                 │  │
│  │  eventsproc     │  │  eventsproc     │  │  eventsproc     │  │
│  │  (RUNNING)      │  │  (STOPPED)      │  │  (STOPPED)      │  │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘  │
│           │                    │                    │            │
│           │         ┌──────────┴──────────┐        │            │
│           │         │    Virtual IP       │        │            │
│           └─────────┤  (Keepalived VIP)   ├────────┘            │
│                     └─────────────────────┘                      │
│                                                                  │
│                     ┌─────────────────────┐                      │
│                     │     PostgreSQL      │                      │
│                     │  (shared checkpoint)│                      │
│                     └─────────────────────┘                      │
└──────────────────────────────────────────────────────────────────┘
```

#### 7.7.2 Failover Behavior

1. **Normal Operation:** Only master runs eventsproc
2. **Master Failure:** Backup promoted, loads checkpoint from DB, resumes processing
3. **No Duplicates:** Single checkpoint ensures events aren't re-sent
4. **Minimal Gap:** Maximum gap = one batch (batch_size events)

---

## 8. Operations Manual

### 8.1 Monitoring

#### 8.1.1 Logs

```bash
# View service logs in real-time
sudo journalctl -u eventsproc -f

# View JSON output
sudo journalctl -u eventsproc -o cat | grep event_id
```

#### 8.1.2 Checkpoint Monitoring

```sql
-- Current status
SELECT
    consumer_id,
    last_event_id,
    total_events_processed,
    processing_node,
    updated_at,
    NOW() - updated_at AS time_since_update
FROM event_processing_checkpoint
ORDER BY consumer_id;

-- Stale checkpoints (>1 hour)
SELECT * FROM event_processing_checkpoint
WHERE updated_at < NOW() - INTERVAL '1 hour';
```

#### 8.1.3 OTEL Collector Monitoring

```bash
# Check OTEL Collector metrics
curl http://localhost:8888/metrics | grep otelcol

# Key metrics:
# - otelcol_receiver_accepted_log_records{receiver="journald"}
# - otelcol_exporter_sent_log_records{exporter="loki"}
# - otelcol_exporter_sent_log_records{exporter="splunk_hec"}
# - otelcol_exporter_send_failed_log_records (should be 0)
```

### 8.2 Common Operations

#### 8.2.1 Reset Checkpoint

**Warning:** This will cause duplicate events in all destinations!

```sql
-- Reset checkpoint (reprocess everything)
UPDATE event_processing_checkpoint
SET last_event_id = 0, total_events_processed = 0, updated_at = NOW()
WHERE consumer_id = 'eventsproc-sandbox-emea';
```

#### 8.2.2 Skip to Specific Event

```sql
UPDATE event_processing_checkpoint
SET last_event_id = 50000, updated_at = NOW()
WHERE consumer_id = 'eventsproc-sandbox-emea';
```

### 8.3 Troubleshooting

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| "postgres_url is required" | Missing config | Set EP_POSTGRES_URL or config file |
| Checkpoint not updating | No new events or process not running | Check events table, verify service running |
| Events not in Loki/Splunk | OTEL Collector issue | Check OTEL logs, verify exporters |
| Duplicate events | Checkpoint reset or multiple instances | Verify single instance, check checkpoint |

---

## 9. Testing Strategy

### 9.1 Unit Tests

```bash
cd eventsproc
make test

# Run specific test
go test -v -run TestSendEvents ./pkg/processor/...
```

### 9.2 Test Coverage

| Package | Coverage | Key Tests |
|---------|----------|-----------|
| `config` | 85%+ | LoadConfig, environment override, validation |
| `processor` | 80%+ | Single writer processing, checkpoint management |
| `stdout` | 90%+ | JSON output, metadata flattening |

### 9.3 Integration Testing

Manual testing against staging environments:

1. **Database connectivity:** Verify event reads
2. **JSON output:** Check journalctl for events
3. **OTEL forwarding:** Verify events in Loki/Splunk
4. **Checkpoint persistence:** Restart and verify resume

---

## 10. Dependencies

### 10.1 Go Modules

| Module | Version | Purpose |
|--------|---------|---------|
| `github.com/spf13/viper` | v1.18+ | Configuration management |
| `github.com/lib/pq` | v1.10+ | PostgreSQL driver |

### 10.2 External Services

| Service | Protocol | Port | Authentication |
|---------|----------|------|----------------|
| PostgreSQL | TCP | 5432 | Password |
| systemd journal | - | - | Local access |

### 10.3 Infrastructure

- keepalived for HA
- systemd for process management and journal
- PostgreSQL for event storage and checkpoints
- OpenTelemetry Collector for log forwarding

---

## 11. Risk Assessment

### 11.1 Identified Risks

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Database unavailable | Events not processed | Low | Retry logic, monitoring |
| OTEL Collector down | Events not forwarded | Medium | Monitor OTEL service, queue in journal |
| Duplicate events | Log noise, storage cost | Low | Checkpoint system |
| Missed events | Gap in audit trail | Low | Checkpoint, monitoring |
| Journal disk full | Processing stops | Low | Configure SystemMaxUse in journald.conf |

### 11.2 Failure Modes

| Component | Failure Mode | System Behavior |
|-----------|--------------|-----------------|
| PostgreSQL | Connection lost | Error logged, retry on next poll |
| Stdout | Write fails | Error returned, checkpoint not updated |
| OTEL Collector | Service down | Events accumulate in journal |
| Journal | Disk full | eventsproc logs error, stops processing |
| Checkpoint | Write fails | Processing stops, prevents duplicates |
| Process | Crash | systemd restarts, resumes from checkpoint |

---

## Appendices

### A. Activity Codes Reference

| Code | Name | Description |
|------|------|-------------|
| 1 | `user.peer.add` | User added a peer |
| 2 | `user.peer.delete` | User deleted a peer |
| 5 | `user.join` | User joined the network |
| 49 | `peer.login.expire` | Peer login expired |
| ... | ... | See `pkg/events/activity.go` for full list |

### B. Sample Queries

#### Loki (LogQL)

```logql
# All events
{service_name="eventsproc"} | json

# Events by user
{service_name="eventsproc"} | json | initiator_email = "john.doe@example.com"

# Peer login expirations
{service_name="eventsproc"} | json | activity_name = "Peer login expired"

# Extract metadata fields
{service_name="eventsproc"} | json | line_format "{{.meta_fqdn}} ({{.meta_ip}})"
```

#### Splunk (SPL)

```spl
# All events
index=netbird_events sourcetype="netbird:event"

# Events by activity
index=netbird_events event.activity_name="Peer login expired"

# Count by activity type
index=netbird_events | stats count by event.activity_name | sort - count

# Extract metadata
index=netbird_events | table _time, event.event_id, event.meta_fqdn, event.meta_ip
```

### C. File Locations

| File | Path | Permissions |
|------|------|-------------|
| Binary | `/usr/local/bin/eventsproc` | 755 |
| Config | `/etc/app/eventsproc/config.yaml` | 640 |
| Secrets | `/etc/sysconfig/eventsproc` | 600 |
| Logs | systemd journal | - |
| OTEL Config | `/etc/otelcol-contrib/eventsproc-config.yaml` | 644 |

### D. Version History

| Version | Date | Changes |
|---------|------|---------|
| 2.0.0 | Jan 2026 | **Major simplification**: Removed direct Loki/Splunk integrations. Single stdout writer outputting JSON. Single checkpoint per consumer (removed per-writer checkpoints). OpenTelemetry Collector architecture for log forwarding. ~600 lines of code removed. Requires migration 003. |
| 1.2.0 | Jan 2026 | Replaced direct journald writer with stdout writer. Events for 'journal' target written to stdout in logfmt format. |
| 1.1.0 | Jan 2026 | Per-writer checkpoints: each writer (journal, loki, splunk) tracks progress independently. Requires migration 002. |
| 1.0.0 | Jan 2026 | Initial release with Journal, Loki, Splunk support |

---

**Document Control:**
- Last Updated: January 2026
- Review Cycle: Quarterly
- Owner: VPN Infrastructure Team
