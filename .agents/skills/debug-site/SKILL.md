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

## Replaying & Simulating `controller.Plan`
Use `cmd/replayplan` to inspect the full 24-hour `Plan` horizon recorded at a specific poll, verify how code changes in `pkg/controller/plan.go` affect historical decisions, or test "what-if" setting overrides:

```bash
# Replay all polls on a specific local date and print full horizon schedules at selected times
go run ./cmd/replayplan --site=<siteID> --date=2026-10-19 --detail-at=12:01,13:23

# Compare the recorded Firestore plan schedule vs. the replayed plan schedule
go run ./cmd/replayplan --site=<siteID> --date=2026-10-19 --detail-at=13:22 --show-recorded-plan

# Replay with in-memory setting overrides (e.g., if the user changed settings after an incident)
go run ./cmd/replayplan --site=<siteID> --date=2026-10-19 \
  --min-soc=20 \
  --period-min-soc="On-Peak=20,Off-Peak=50" \
  --export-margin=0 \
  --detail-at=12:01
```

Useful flags:
* `--detail-at=HH:MM,HH:MM` (or `all`): Dump the step-by-step `plan.Periods` table for matching local poll times.
* `--show-recorded-plan`: Print the `action.Plan` stored in Firestore alongside the newly simulated plan.
* `--chain-actions=false`: Feed the historically recorded previous action as `lastAction` instead of chaining the simulated action across polls.
* Setting override flags: `--min-soc`, `--period-min-soc`, `--export-margin`, `--arbitrage-diff`, `--always-charge-under`, `--grid-charge`, `--grid-export-batteries`, `--grid-export-solar`.

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
