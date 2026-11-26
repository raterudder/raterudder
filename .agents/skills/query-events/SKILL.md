---
name: query-events
description: >-
  Query and aggregate RateRudder structured JSON log events in Google Cloud Logging
  (project raterudder) to answer activity questions like how many sites were created,
  users signed up/deleted, ESS systems connected, or notification devices subscribed
  over a time period.
---

# Querying RateRudder Product & Lifecycle Events

Use this skill whenever the user asks about product activity, counts, or trends over a time period (e.g., "how many sites were created last week?", "how many users connected an ESS this month?", "give me a weekly summary of events", "how many devices subscribed to notifications?").

## Source of Truth
All emitted event names are defined as constants in `pkg/server/events.go` and logged via `log.Ctx(ctx).InfoContext(...)` under the top-level JSON field `"event"` (`jsonPayload.event` in Cloud Logging).

## Event Catalog

| `jsonPayload.event` | Meaning / Trigger | Key `jsonPayload` fields | Handler |
|---|---|---|---|
| `site_update_heartbeat` | `/api/updateSites` cron run completed successfully | `kind`, `idx`, `total`, `siteCount` | `pkg/server/update.go` |
| `site_created` | New site created via `/api/join` | `siteID`, `userID` | `pkg/server/join.go` |
| `user_created` | New user account registered via `/api/join` | `userID`, `email` | `pkg/server/join.go` |
| `user_joined_site` | User joined an existing site via invite code | `siteID`, `userID`, `email` | `pkg/server/join.go` |
| `site_deleted` | Site deleted via `/api/delete/site` | `siteID`, `deletedBy` | `pkg/server/delete.go` |
| `user_deleted` | User account deleted via `/api/delete/user` | `userID` | `pkg/server/delete.go` |
| `ess_connected` | First time an ESS is configured & verified on a site | `siteID`, `userID`, `ess` | `pkg/server/settings.go` |
| `utility_configured` | First time a utility provider is configured on a site | `siteID`, `userID`, `utilityProvider`, `utilityRate` | `pkg/server/settings.go` |
| `site_automation_paused` | Connected site paused (`Pause: false -> true`) | `siteID`, `userID` | `pkg/server/settings.go` |
| `site_automation_resumed` | Connected site resumed (`Pause: true -> false`) | `siteID`, `userID` | `pkg/server/settings.go` |
| `notification_device_subscribed` | Push notifications subscribed on a new device | `userID`, `appType` | `pkg/server/notifications.go` |
| `notification_device_unsubscribed` | Push subscription removed | `userID` | `pkg/server/notifications.go` |
| `feedback_submitted` | User submitted feedback | `userID`, `siteID`, `sentiment` | `pkg/server/feedback.go` |
| `interest_submitted` | User submitted utility/battery interest form | `utility`, `battery`, `state` | `pkg/server/interest.go` |
| `app_connected` | API key created or OAuth app authorized & redeemed | `appID`, `appName`, `appType` (`apiKey` / `oauth`), `siteID`, `userID` | `pkg/server/apps.go`, `pkg/server/oauth.go` |
| `app_revoked` | Connected app revoked | `appID`, `siteID`, `userID` | `pkg/server/apps.go` |

## How to Query in Google Cloud Logging

* **GCP Project**: `raterudder` (`resourceNames: ["projects/raterudder"]`)
* **Tool**: Use MCP `google-cloud-logging` `list_log_entries` (or `gcloud logging read` if needed).
* **Pagination**: Follow `nextPageToken` until exhausted so counts over multi-day windows are accurate.
* **Exclude heartbeats for activity summaries**: `site_update_heartbeat` fires on every `/api/updateSites` cron invocation; filter it out when summarizing user/product events.

### Example Cloud Logging Filters

* **Single event over a date range** (e.g. sites created in the last 7 days):
  ```text
  jsonPayload.event="site_created" AND timestamp >= "YYYY-MM-DDTHH:MM:SSZ"
  ```
* **All product/lifecycle events (excluding heartbeats)**:
  ```text
  jsonPayload.event:* AND jsonPayload.event!="site_update_heartbeat" AND timestamp >= "YYYY-MM-DDTHH:MM:SSZ"
  ```
* **ESS connections broken down by provider**:
  ```text
  jsonPayload.event="ess_connected" AND timestamp >= "YYYY-MM-DDTHH:MM:SSZ"
  ```
  Group returned entries by `jsonPayload.ess` (e.g., `franklin`, `tesla`, `enphase`).
* **Notification device subscriptions by app container**:
  ```text
  jsonPayload.event="notification_device_subscribed" AND timestamp >= "YYYY-MM-DDTHH:MM:SSZ"
  ```
  Group returned entries by `jsonPayload.appType` (`webapk`, `standalone`, `browser`).
* **Restrict to a specific Cloud Run service** (e.g., exclude staging if both log to `projects/raterudder`):
  ```text
  resource.type="cloud_run_revision" AND resource.labels.service_name="raterudder" AND jsonPayload.event="site_created" AND timestamp >= "YYYY-MM-DDTHH:MM:SSZ"
  ```

## Adding New Events
When adding a new event to `pkg/server/events.go`:
1. Use `snake_case` for the event name and headless `camelCase` for extra `slog.*` fields.
2. Emit only after the underlying storage write succeeds.
3. Update the Event Catalog table in this file so future queries stay in sync.
