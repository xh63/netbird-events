# GitHub Sanitization Report

**Generated:** Tue Feb  3 18:01:11 AEDT 2026
**Source:** /Users/david/git/ingenico/usrvpn/code/netbird-prov/eventsproc
**Output:** /tmp/netbird-events-sanitized-90134

## Summary

- Configuration: /Users/david/git/local/github-sanitizer/sanitize.conf
- Files processed: 0
- Verification issues: 0

## What Was Done

### Files Removed
- .gitlab-ci.yml
- .DS_Store
- Thumbs.db
- config.yaml
- config.local.yaml
- .env
- .env.local
- config.alloy.commented
- config.alloy.otlp
- IMPLEMENTATION_SUMMARY.md
- PLATFORM-REGION-USAGE.md
- PUSH-TO-GITHUB.md
- docs/ACTIVITY_ENRICHMENT.md
- docs/CONTEXT_FIELDS.md
- docs/EVENTSPROC_ENRICHMENT.md
- docs/EVENTSPROC_QUICKSTART.md
- docs/EVENTSPROC_README.md
- docs/EVENTSPROC_SUMMARY.md
- docs/EVENTSPROC_TECH_DOC.md
- docs/EVENTSPROC_TESTING.md
- docs/EVENTSPROC_TEST_WORKFLOW.md
- docs/EVENTSPROC_WORKFLOW.md
- docs/HTTP_VS_OTLP_COMPARISON.md
- docs/LOGGING_ARCHITECTURE.md
- docs/LOKI_MTLS.md
- docs/LOKI_OTLP_SETUP.md
- docs/LOKI_QUERIES.md
- docs/MTLS_SUMMARY.md
- docs/MULTI_TARGET_LOGGING.md
- docs/OTEL_COLLECTOR_SETUP.md
- docs/README.md
- docs/SPLUNK_INTEGRATION.md
- docs/SQLMOCK_EXPLAINED.md
- docs/TESTING_GUIDE.md
- docs/TEST_WORKFLOW.md
- docs/WORKFLOW.md
- bin/
- dist/
- ssl/
- certs/

### Patterns Replaced

**Hostnames:**
- `s/usrvpn-netbird\.pg\.services\.dbaas\.sb\.eu\.ginfra\.net/postgres.example.com/g`
- `s/usrvpn-netbird\.pg\.services\.dbaas\.sb\.au\.ginfra\.net/postgres.example.com/g`
- `s/loki\.services\.core\.sb\.eu\.ginfra\.net:3222/loki.example.com:3100/g`
- `s/splunk-hf\.services\.sectools\.sb\.eu\.ginfra\.net/splunk-hec.example.com/g`
- `s/redis-netbird\.services\.usrvpn\.sb\.au\.ginfra\.net/redis.example.com/g`
- `s/[a-zA-Z0-9_-]*\.pg\.services\.dbaas\.[a-z.]*\.ginfra\.net/postgres.example.com/g`
- `s/\.pg\.services\.dbaas\.[a-z.]*\.ginfra\.net/postgres.example.com/g`
- `s/netbird\.offnet\.[a-zA-Z0-9.-]*\.giservices\.io/netbird.example.com/g`
- `s/netbird\.offnet\.[^ \t"']*/netbird.example.com/g`
- `s/\.ginfra\.net/.example.com/g`
- `s/\.giservices\.io/.example.com/g`
- `s/\.sb\.eu\.example\.com/.example.com/g`
- `s/\.sb\.au\.example\.com/.example.com/g`
- `s/\.pp\.eu\.example\.com/.example.com/g`
- `s/\.pp\.au\.example\.com/.example.com/g`
- `s/\.pr\.eu\.example\.com/.example.com/g`
- `s/\.pr\.au\.example\.com/.example.com/g`
- `s/\.services\.[a-z]*\.example\.com/.example.com/g`
- `s/[a-zA-Z0-9_-]*\.example\.com\.example\.com/db.example.com/g`

**Paths:**
- `s|/etc/app|/etc/app|g`
- `s|/var/log/usrvpn|/var/log/app|g`
- `s|/opt/app|/opt/app|g`
- `s|/Users/david/git/ingenico/usrvpn/code/netbird-prov/||g`
- `s|/home/pierre/git/ingenico/usrvpn/code/netbird-prov/||g`
- `s|/Users/david/git/[^/]*/[^/]*/code/[^/]*/||g`
- `s|/home/pierre/git/[^/]*/[^/]*/code/[^/]*/||g`
- `s|/Users/[^/]*/git/||g`
- `s|/home/[^/]*/git/||g`

**Users/Organizations:**
- `s/user=usrvpn_netbird_own/user=netbird_user/g`
- `s/usrvpn_netbird_own/netbird_user/g`
- `s/dbname=usrvpn_netbird/dbname=netbird/g`
- `s/usrvpn_netbird/netbird/g`
- `s/usrvpn_/app_/g`
- `s/custom organization/Company/g`
- `s/ingenico/company/gi`
- `s/\/usrvpn\//\/netbird\//g`
- `s/usrvpn\.example\.com/app.example.com/g`
- `s/nbmgmt[0-9]*e[0-9]*/netbird-mgmt-emea-1/g`
- `s/nbmgmt[0-9]*a[0-9]*/netbird-mgmt-apac-1/g`
- `s/ais\.okta_users/okta_users/g`
- `s/SECRET_FROM_HIERA/SECRET/g`
- `s/Running tests for eventsproc and pkg\.\.\.$/Running tests.../g`
- `s/@$(GO) test -v -cover \.\/\.\. \.\.\/pkg\/events\/\.\.\. \.\.\/pkg\/stdout\/\.\.\./@$(GO) test -v -cover .\/\.\./g`

**Sensitive Data:**
- `s/password=[^ ]*/password=YOUR_PASSWORD_HERE/g`
- `s/token=[^ ]*"/token=YOUR_TOKEN_HERE"/g`
- `s/bearer_token = "[^"]*"/bearer_token = "YOUR_TOKEN_HERE"/g`
- `s/netbird_token: "[^"]*"/netbird_token: "YOUR_TOKEN_HERE"/g`
- `s/okta_token: "[^"]*"/okta_token: "YOUR_TOKEN_HERE"/g`
- `s/netbox_token:/netbox_token: YOUR_TOKEN_HERE #/g`
- `s/readthissecrectfromhiera/YOUR_PASSWORD_HERE/g`

## Next Steps

1. Review the sanitized code in: `/tmp/netbird-events-sanitized-90134`
2. Check for any remaining sensitive information
3. Update LICENSE copyright if needed
4. Test the build: `make build` or equivalent
5. Initialize git repository:
   ```bash
   cd /tmp/netbird-events-sanitized-90134
   git init
   git add .
   git commit -m "Initial commit"
   ```
6. Create GitHub repository and push

## File Statistics

```
Total files:    27
Go files:       6
Markdown files: 7
YAML files:     2
```
