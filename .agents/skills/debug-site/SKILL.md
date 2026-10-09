---
name: debug-site
description: >-
  Runbook and procedures for debugging user-reported site issues, checking Cloud Logging
  (project raterudder), inspecting Firestore settings, and analyzing Decide/Plan battery actions.
---

# Site Debugging Runbook

Use this runbook when the user asks to investigate a specific site issue (e.g., "battery didn't charge overnight", "why did battery export", "check site <siteID>").

## Quick Diagnostic Tool
Run the inspection tool to dump current settings and recent action history in the site's local timezone:
```bash
go run ./cmd/inspectsite --site=<siteID> --days=3
```
You can also specify explicit date bounds:
```bash
go run ./cmd/inspectsite --site=<siteID> --start=2026-10-04 --end=2026-10-07
```

## Key Investigation Steps

### 1. Identify Environment & Timezone
* **Timezone**: Look up `SystemStatus.TimeLocation` (e.g., `America/Los_Angeles`, `America/Chicago`). "Overnight" must be evaluated relative to the site's local timezone, not UTC.
* **Control Mode**: Check `settings.PlanMode`. If `false`, the site runs legacy `controller.Decide`. If `true`, it runs `controller.Plan`.

### 2. Check Automation Status
* Check if `settings.Pause` is `true`. When paused, the battery remains in self-consumption (`BatteryModeLoad`) and no automated grid charging/export occurs.

### 3. Google Cloud Logging Queries
* **GCP Project**: `raterudder` (Resource container: `projects/raterudder`).
* **Tool**: Use MCP `google-cloud-logging` `list_log_entries`.
* **Helpful Filters**:
  * All logs for site: `"<siteID>" timestamp >= "YYYY-MM-DDTHH:MM:SSZ"`
  * Check for ESS/API errors: `"<siteID>" ("502 Bad Gateway" OR "site update failed" OR "ESS rate limited" OR "login failed")`
  * Check for settings changes: `"<siteID>" "settings updated"`
  * Check for pause events: `"<siteID>" "update: paused"`

### 4. Privacy Guardrail
* Never commit site IDs, customer names, or user emails to git repository files, test fixtures, or commit messages.
