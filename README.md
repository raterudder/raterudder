# RateRudder

RateRudder is a home energy management system that automates residential Energy Storage Systems (ESS)—such as Tesla Powerwall, FranklinWH aPower, and Enphase IQ Battery—based on real-time wholesale and Time-of-Use (TOU) electricity pricing.

Standard battery modes like Self-Consumption only react to local solar and home load without awareness of utility rate schedules, while static time-based rules charge and discharge on fixed timers regardless of upcoming weather or dynamic pricing. RateRudder periodically fetches utility tariffs, weather forecasts, and battery telemetry to compute and apply an updated 24-hour dispatch schedule through the manufacturer's cloud API.

## Overview & Capabilities

- **24-Hour Dispatch Planner (`pkg/controller`)**: Uses dynamic programming over sub-hour intervals to schedule when the battery should charge from the grid, remain in standby, power the home, or export solar and stored energy to the grid.
- **Physical & Economic Constraints**: Accounts for AC/DC round-trip efficiency losses, battery cycle degradation cost, pre-peak charging buffers, and mode-transition penalties to avoid frequent inverter relay switching during minor wholesale price fluctuations.
- **Solar Production Modeling**: Estimates site-specific solar generation from weather irradiance forecasts and historical production data, automatically adjusting for panel orientation, inverter clipping, and seasonal shading without requiring manual array specifications.
- **Home Load & EV Detection**: Predicts hourly household consumption from historical usage and temperature forecasts—accounting for weekday vs. weekend patterns and low-load vacation periods—while isolating high-draw EV charging spikes so the home battery does not discharge into a vehicle.
- **Backup Reserves & VPP Pre-Charging**: Enforces a configurable minimum state-of-charge reserve, supports time-windowed reserve floors (such as allowing deeper discharge during evening peak hours while maintaining a higher reserve overnight), and pre-charges ahead of scheduled Virtual Power Plant (VPP) events.
- **Counterfactual Simulation**: Simulates baseline battery behavior alongside actual operation to estimate the financial difference between RateRudder's dispatch decisions and default self-consumption.
- **Notifications**: Supports Web Push notifications for price spikes, negative pricing windows, grid outages, abnormal load events, and daily status summaries.

## Supported Integrations

### Energy Storage Systems (`pkg/ess`)
All battery integrations implement the `ess.System` interface:
- **Tesla**: Powerwall 2, Powerwall+, and Powerwall 3 (via Tesla Fleet API)
- **FranklinWH**: aPower / aGate systems
- **Enphase**: IQ Battery systems

### Utilities & Tariffs (`pkg/utility`)
All tariff integrations implement the `utility.Utility` interface. The repository includes implementations for over 50 utilities and 120 rate plans across the US, UK, and Australia, as well as user-defined TOU schedules:
- **Real-Time & Wholesale Markets**: ComEd, Ameren Illinois, ERCOT / Tesla Electric, PJM and MISO day-ahead/real-time markets, Octopus Energy (Agile), Amber Electric, and Xcel Energy.
- **Time-of-Use & Tiered Tariffs**: PG&E, Southern California Edison (SCE), SDG&E, Duke Energy, Eversource, FPL, BGE, PECO, Pepco, APS, SRP, Dominion, PSE, ConEd, and others.

### Weather & Geocoding (`pkg/weather`)
Weather and location lookups sit behind the `weather.Service` interface, allowing underlying providers to be swapped out if needed:
- **Solar & Temperature Forecasts**: Fetched from Open-Meteo (including global horizontal, direct normal, and diffuse solar irradiance as well as ambient temperature).
- **Geocoding & Timezones**: Resolved from site postal codes using Open-Meteo Geocoding by default, with optional support for the Google Maps Geocoding and Time Zone APIs.

### Storage & Infrastructure (`pkg/storage`)
RateRudder is built to run as a stateless service on Google Cloud Run with Google Cloud Firestore as its primary database, triggered periodically by Cloud Scheduler. However, all persistence operations in `pkg/storage` are defined against the `storage.Database` interface, allowing other databases to be added without changing the controller or HTTP server packages.

## Repository Structure

- `cmd/` — Main application binary and operational CLI utilities:
  - `cmd/raterudder`: HTTP server and periodic site orchestration entry point.
  - `cmd/inspectsite`: CLI tool to inspect site configuration, recent actions, and pricing history.
  - `cmd/replayplan`: CLI tool to replay `controller.Plan` against historical poll snapshots.
  - `cmd/fetchhistory`, `cmd/decryptcreds`, `cmd/setplanmode`, `cmd/migrate*`: Administrative and migration tools.
- `pkg/` — Backend Go packages:
  - `pkg/controller`: Dispatch planning (`plan.go`), solar forecasting (`solar.go`), load modeling (`energy.go`), EV detection (`ev.go`), and baseline simulation (`simulation.go`).
  - `pkg/ess`: Battery provider interface and client implementations.
  - `pkg/utility`: Utility rate fetchers and tariff schedule evaluators.
  - `pkg/weather`: Weather forecast and geocoding interface and clients.
  - `pkg/storage`: `Database` interface and Firestore implementation.
  - `pkg/server`: HTTP API handlers, OIDC authentication (Google/Apple), credential encryption, Web Push delivery, and static asset serving.
  - `pkg/types`: Shared domain structs and interfaces.
  - `pkg/common` & `pkg/log`: HTTP client helpers, context utilities, and structured logging (`log/slog`).
- `web/` — React, TypeScript, and Vite single-page application using `@base-ui/react` and Recharts. Production builds are embedded directly into the Go binary via `web/embed.go`.
- `examples/tf/` — Example Terraform configuration for deploying on Google Cloud (Cloud Run, Cloud Scheduler, Firestore, and Secret Manager).

## Testing & Real-Data Benchmarks

In addition to standard unit and integration tests, `pkg/controller` includes historical replay benchmarks backed by anonymized multi-week telemetry datasets from real sites (`pkg/controller/testdata/`):

- **Plan & Controller Replay (`plan_history_test.go`, `controller_history_test.go`)**: Replays historical site telemetry, weather forecasts, and utility prices hour-by-hour through the planner, simulating battery state-of-charge trajectories and comparing total electricity cost against per-site and fleet-wide baselines.
- **Load Forecasting Evaluation (`energy_history_test.go`)**: Evaluates `BuildHourlyEnergyModel` across recorded site histories at multiple daily planning hours, tracking Mean Absolute Error (MAE) alongside total under-prediction and over-prediction (kWh) to guard against regressions across heatwaves, weekend shifts, EV charging, and vacation days.

## Development

### Prerequisites
- Go 1.27+
- Node.js & npm
- Google Cloud Firestore (or the local Firestore Emulator via `firebase-tools` / `gcloud` for local runs and storage tests)

### Building & Running Tests

Frontend (`web/`):
```bash
cd web
npm install
npm test        # Run Vitest suite
npm run build   # Build static assets for Go embedding
```

Backend (`cmd/` & `pkg/`):
```bash
go build ./cmd/raterudder
go test ./pkg/... ./cmd/...
```
Storage tests in `pkg/storage` connect to the local Firestore Emulator (`npx firebase-tools emulators:start --only firestore --project demo-test`).

To view all configuration flags and environment variables:
```bash
./raterudder --help
```

## License

See [LICENSE](LICENSE) for details.
