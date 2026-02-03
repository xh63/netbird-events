# Grafana Alloy Setup for Application Log Collection

This guide covers setting up Grafana Alloy to collect application logs from systemd journal and forward them to both Loki (with mTLS) and Splunk HEC.

## Overview

Grafana Alloy will:
- Read logs from systemd journal
- Filter out system logs (keep only application logs)
- Send logs to Loki using mTLS authentication
- Send logs to Splunk HEC using custom CA certificate

## Prerequisites

1. **Certificates in place:**
   ```
   /etc/app/eventsproc/ssl/rw-cert.pem    # Loki client cert
   /etc/app/eventsproc/ssl/rw-key.pem     # Loki client key
   /etc/app/eventsproc/ssl/rw-ca.pem      # Loki CA cert
   /etc/app/eventsproc/ssl/splunk_ca.pem  # Splunk CA cert
   ```

2. **Splunk HEC Token:**
   - Obtain HEC token from Splunk admin
   - Set as environment variable (see below)

## Installation

### Install Grafana Alloy

```bash
# On Debian/Ubuntu
sudo mkdir -p /etc/apt/keyrings/
wget -q -O - https://apt.grafana.com/gpg.key | gpg --dearmor | sudo tee /etc/apt/keyrings/grafana.gpg > /dev/null
echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" | sudo tee /etc/apt/sources.list.d/grafana.list
sudo apt-get update
sudo apt-get install -y alloy

# On RHEL/CentOS
sudo tee /etc/yum.repos.d/grafana.repo << EOF
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
EOF
sudo yum install -y alloy
```

### Configure Alloy

1. **Copy the configuration:**
   ```bash
   sudo cp config.alloy /etc/alloy/config.alloy
   ```

2. **Configure Splunk HEC Token:**

   The configuration uses a placeholder `__SPLUNK_HEC_TOKEN__` that should be replaced during deployment.

   **For Puppet deployment:**
   Replace the placeholder in the config file:
   ```puppet
   file { '/etc/alloy/config.alloy':
     ensure  => file,
     content => template('alloy/config.alloy.erb'),
     # Or use file() with regsubst to replace __SPLUNK_HEC_TOKEN__
   }
   ```

   **For manual deployment:**
   ```bash
   sed -i 's/__SPLUNK_HEC_TOKEN__/your-actual-token-here/' /etc/alloy/config.alloy
   ```

3. **Verify certificate permissions:**
   ```bash
   sudo chown alloy:alloy /etc/app/eventsproc/ssl/*.pem
   sudo chmod 600 /etc/app/eventsproc/ssl/*.pem
   ```

## Configuration Customization

### Add More Application Services

The default configuration only reads from `eventsproc.service`. To read from additional services, update the `matches` parameter:

```hcl
loki.source.journal "application_logs" {
  // Single service (default)
  matches = "_SYSTEMD_UNIT=eventsproc.service"

  // Multiple specific services (use this format)
  // matches = "_SYSTEMD_UNIT=eventsproc.service|_SYSTEMD_UNIT=myapp.service"
}
```

**Or create multiple sources for different services:**

```hcl
loki.source.journal "eventsproc_logs" {
  matches = "_SYSTEMD_UNIT=eventsproc.service"
  labels = { app = "eventsproc" }
  forward_to = [loki.process.filter_and_format.receiver]
}

loki.source.journal "myapp_logs" {
  matches = "_SYSTEMD_UNIT=myapp.service"
  labels = { app = "myapp" }
  forward_to = [loki.process.filter_and_format.receiver]
}
```

### Add Additional Labels

To add more labels to all logs:

```hcl
loki.source.journal "application_logs" {
  labels = {
    job         = "systemd-journal",
    host        = env("HOSTNAME"),
    environment = "production",    // Add this
    region      = "eu",             // Add this
  }
}
```

### Adjust Batching and Performance

Modify batch settings for higher throughput:

```hcl
endpoint {
  batch_size = 2097152  // 2MB (increase from 1MB)
  batch_wait = "500ms"  // Send more frequently
}
```

## Running Alloy

### Start and Enable Service

```bash
# Start Alloy
sudo systemctl start alloy

# Enable on boot
sudo systemctl enable alloy

# Check status
sudo systemctl status alloy
```

### View Logs

```bash
# Follow Alloy logs
sudo journalctl -u alloy -f

# View recent errors
sudo journalctl -u alloy -p err -n 50
```

### Validate Configuration

Before starting, validate the configuration syntax:

```bash
sudo alloy fmt --check /etc/alloy/config.alloy
```

## Testing

### Test Journal Reading

Check that Alloy is reading from journal:

```bash
# Generate test log
logger -t myapp "Test log message from myapp"

# Check if Alloy picked it up (look for "myapp" in Alloy logs)
sudo journalctl -u alloy -n 20
```

### Test Loki Connection

Query Loki to verify logs are arriving:

```bash
# Using LogCLI (if available)
logcli query '{job="systemd-journal"}' --limit=10 --addr=https://loki.example.com:25430

# Or check Grafana Explore interface
```

### Test Splunk Connection

Search in Splunk:
```
index=* source="systemd-journal" sourcetype="_json"
```

## Troubleshooting

### Alloy Not Starting

```bash
# Check configuration syntax
sudo alloy fmt --check /etc/alloy/config.alloy

# Check for detailed errors
sudo journalctl -u alloy -n 100 --no-pager
```

### Certificate Errors

```bash
# Verify certificate files exist and are readable
sudo -u alloy cat /etc/app/eventsproc/ssl/rw-cert.pem

# Check certificate validity
openssl x509 -in /etc/app/eventsproc/ssl/rw-cert.pem -noout -dates
```

### No Logs Appearing

1. **Check if Alloy is reading journal:**
   ```bash
   sudo journalctl -u alloy | grep "journal"
   ```

2. **Verify filtering isn't too aggressive:**
   Temporarily comment out the `stage.match` drop block to see all logs

3. **Check network connectivity:**
   ```bash
   # Test Loki
   curl -v --cert /etc/app/eventsproc/ssl/rw-cert.pem \
        --key /etc/app/eventsproc/ssl/rw-key.pem \
        --cacert /etc/app/eventsproc/ssl/rw-ca.pem \
        https://loki.example.com:25430/ready

   # Test Splunk
   curl -v --cacert /etc/app/eventsproc/ssl/splunk_ca.pem \
        https://splunk-hec.example.com/services/collector/event
   ```

### High Memory Usage

Reduce batch sizes:
```hcl
batch_size = 524288  // 512KB
```

Or adjust queue settings (add to endpoint block):
```hcl
queue_config {
  capacity = 500
  max_age  = "1m"
}
```

## Monitoring

### Check Alloy Metrics

Alloy exposes metrics at `http://localhost:12345/metrics` by default.

Key metrics to monitor:
- `loki_write_sent_entries_total` - Logs sent to Loki
- `loki_write_dropped_entries_total` - Logs dropped
- `loki_write_request_duration_seconds` - Write latency

### Health Check

```bash
curl http://localhost:12345/-/healthy
```

## Security Considerations

1. **Protect certificates:**
   - Ensure proper permissions (600 or 400)
   - Rotate certificates before expiration

2. **Secure HEC token:**
   - Never commit actual tokens to git (use placeholders)
   - Puppet should replace `__SPLUNK_HEC_TOKEN__` during deployment
   - Ensure config file has restricted permissions (600 or 640)
   - Rotate tokens periodically

3. **Limit journal access:**
   - Alloy runs as `alloy` user
   - Ensure user has read access to journal but not write

## Reading All Application Logs (Alternative)

If you want to read ALL application logs and filter out system services, you can use a broader approach:

```hcl
loki.source.journal "all_apps" {
  // Match all systemd units
  matches = "_SYSTEMD_UNIT"

  labels = {
    job = "systemd-journal",
  }

  forward_to = [
    loki.process.filter_system.receiver,
  ]
}

loki.process "filter_system" {
  // Drop common system services
  stage.match {
    selector = "{unit=~\"(systemd.*|kernel|dbus.*|networkd|resolved|sshd.*|cron.*)\\..*\"}"
    action   = "drop"
  }

  forward_to = [
    loki.process.filter_and_format.receiver,
  ]
}
```

Note: The default config is more efficient as it only reads eventsproc.service logs.
