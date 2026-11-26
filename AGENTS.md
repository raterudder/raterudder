# Agent Guidelines & Project Standards

## Architecture & Project Layout
- `cmd/raterudder`: Main entry point and orchestration loop (site updates run every 20 minutes via Cloud Scheduler calling `/api/updateSites`).
- `pkg/controller`: Battery charging/discharging decision logic and scheduling simulations.
- `pkg/utility`: Electricity tariff fetchers (e.g., ComEd hourly/real-time pricing).
- `pkg/ess`: Energy Storage System integrations (FranklinWH, Tesla, etc.).
- `pkg/feats`: Per-environment boolean feature flagging and site evaluation (`features/{release}` in Firestore).
- `pkg/storage`: Persistence layer using Google Cloud Firestore.
- `pkg/server`: HTTP API endpoints for notifications, settings, history, auth, and telemetry.
- `pkg/model` & `pkg/types`: Shared domain models, tariff structures, and interfaces.
- `web`: React frontend utilizing `@base-ui/react`.

---

## Planning & Design Principles (Save Time & Reduce Revisions)
- **Reuse Existing Types & Slice Collections**:
  - Check `pkg/types` (e.g., `TimePeriod`) before introducing new bespoke structs.
  - Prefer slices of existing shared types (e.g., `[]TimePeriod`) rather than single-instance structs or redundant `Enabled: bool` flags. An empty slice naturally represents "disabled" and future-proofs the schema for multi-window support.
- **Unified Query Methods**:
  - Avoid adding one-off, type-specific helper methods on notification/state wrappers (e.g., do not add `lastGridEvent()` or `lastPriceSpikeLog()`).
  - Provide a single, generalized query method (e.g., `lastLog(userID string, onlyDelivered bool, notifTypes ...string) (types.NotificationLog, bool)`) that returns the full log struct and a boolean `found` indicator.
- **Early Exit Before DB Queries**:
  - In notification and polling loops, evaluate cooldowns, recent log states, and quiet periods *before* calling expensive database operations like `GetUser`, avoiding unnecessary I/O and unexpected mock expectations in tests.

---

## Backend & Go Style
- **Formatting**: Use standard `go fmt` formatting and standard Go import groupings. Always run `go fmt ./pkg/...` on changed files.
- **Package Boundaries**: Keep controller logic in `pkg/controller`, and external I/O in `utility`, `ess`, `storage`, or `server`.
- **APIs**: Prefer `context.Context` as the first argument in public functions and return `(value, error)`.
- **Logging**:
  - Use `log/slog` with typed attribute helpers: `slog.String`, `slog.Int`, `slog.Float64`, `slog.Bool`, `slog.Time`, `slog.Duration`, `slog.Any`.
  - Always format log field names in headless camelCase (e.g., `userID`, `batterySOC`, `homeKW`).
- **Structs & JSON**: Struct field JSON tags must be headless camelCase.
- **Naming & Units**: Always make the unit explicit in variable, struct field, and method names. Avoid ambiguous names like `Rate` or `Price` without units (which could be cents or dollars); specify the unit explicitly (e.g., `ImportRateDollars` and `ExportRateDollars` or `ImportDollars` and `ExportDollars`, `DollarsPerKWH`, `LoadKWH`, `batterySOC`, `DurationHours`, `DurationSeconds`, `homeKW`).
- **Struct Comparisons with Slices**: In Go, structs containing slice fields (e.g., `UserNotificationSettings` with `QuietPeriods []TimePeriod`) cannot be compared with `==`. Use `reflect.DeepEqual(a, b)` in storage/persistence checks.

---

## Feature Flags (`pkg/feats`)
- **Defining Flags**:
  - Register flags as package-level variables in the package where they are evaluated using `feats.Bool("<flagName>", <defaultValue>)` (e.g., `var ffSolarModelV2 = feats.Bool("solarModelV2", false)`).
  - Flag names must be unique and formatted in headless camelCase. Calling `feats.Bool` with the same name and different default values panics at startup.
- **Backend Evaluation**:
  - Check whether a flag is enabled using `ffSolarModelV2.Enabled(ctx)`.
  - Request and background poll contexts are automatically populated with evaluated site flags by `Server.initFeats(ctx, siteID)` (cached per instance and synced to 5-minute boundaries against `features/{release}` in Firestore, or `features/{features-environment}` when `-features-environment` is configured). If a flag is not overridden in `ctx`, `Enabled(ctx)` returns the flag's registered default value.
- **Overriding in Go Tests**:
  - Override a flag in unit tests without needing storage or an `Evaluator` by wrapping the context with `With`:
    `ctx = ffSolarModelV2.With(ctx, true)`
- **Frontend Usage**:
  - Evaluated flags for the active site are returned in `GET /api/settings` (`SettingsRes.Features`) and provided to the React tree via `FeaturesProvider` in `web/src/utils/features.tsx`.
  - Read a single flag in components with `const enabled = useFeatureFlag('solarModelV2')` or all flags with `const features = useFeatures()`.

---

## Testing Guidelines

### Go Tests
- **Offline Unit Tests with Race Detector**: Run fast offline unit tests across all packages (skipping controller history replays and live external API calls):
  `go test -race -short -skip '^Test(Plan|Energy)History$' ./...`
- **Controller Historical Replays**: Run recorded plan and energy model replay datasets separately without `-race`:
  `go test ./pkg/controller -run '^Test(Plan|Energy)History$'`
- **Live External API Tests (Opt-In)**: Live weather and utility provider subtests are skipped by default and under `-short`. Opt in explicitly when verifying upstream APIs:
  `RATERUDDER_LIVE_API_TESTS=1 go test ./pkg/weather ./pkg/utility -run '^Test(OpenMeteoService|OctopusUtility|ComEd|Ameren)$/^Integration_Real'`
- **Run Single Tests**: Run a single test at a time using regex:
  `go test ./pkg/server -run "^TestFunctionName$"`
  (Do not pass `-name`; `go test` uses `-run`).
- **Never Pass `-v`**: Avoid passing `-v` to `go test` to prevent noisy output.
- **Never Redirect Output**: Do not redirect test output to a file (no `> out.txt`).
- **Subtests with `t.Run`**:
  - Tests for a function `fooBar` defined in `<file>.go` must live in `<file>_test.go` inside a top-level test function named `Test<FunctionName>` (e.g., `handleListFeatures` and `handleUpdateFeatures` in `admin.go` must be tested by `TestHandleListFeatures` and `TestHandleUpdateFeatures` in `admin_test.go`, not grouped into another file or under an unrelated `Test*` function).
  - **Never create separate top-level test functions for feature variants or subtests of existing functions** (e.g., do not create `TestHandleGridOutageQuietPeriod`; add `t.Run("QuietPeriod...", ...)` inside `TestHandleGridOutageNotifications`).
  - Avoid looping over slices of test case structs. Prefer explicit, named subtests with `t.Run`.
- **Testify Assertions**:
  - Use `require.NoError(t, err)` for setup/preconditions where failure must stop execution.
  - Use `assert.ErrorContains(t, err, "expected error")` for checking error messages.
  - Wrap dependent assertions in an `if` block:
    `if assert.Len(t, slice, 2) { assert.Equal(t, "expected", slice[0]) }`
- **Auth & Mock Handlers**:
  - When testing HTTP handlers that use `authMiddleware` (with `bypassAuth: false`), use `setupOIDCTest(t)` and `generateTestToken` to create mock OIDC servers and valid JWT cookies in `httptest.NewRequest`. Do not rely solely on injecting `context.WithValue`.
- **Firestore Emulator**:
  `npx firebase-tools emulators:start --only firestore --project demo-test`
- **Go Binary Compilation**:
  - Compile and link all server and CLI packages under `cmd/`:
    `go build -tags timetzdata ./cmd/...`
  - Build static production `raterudder` binary with embedded `web/dist` (run frontend build first):
    `CGO_ENABLED=0 GOOS=linux go build -tags timetzdata -trimpath -ldflags='-s -w' -o /tmp/raterudder ./cmd/raterudder`
- **Fixture Privacy Checks**:
  - Verify synthetic `^site[0-9]+$` IDs, redacted PII/credentials, and 2-decimal weather coordinates across `pkg/controller/testdata/`:
    `go test ./pkg/controller -run "^TestFixturePrivacy$"`

### Secret & Privacy Scans
- **Synthetic Test Credentials**: Prefix all fake API keys, tokens, and secrets in unit tests (`*_test.go`, `*.test.ts(x)`, `web/src/test/*`) with `MOCK_`, `FAKE_`, `mock-`, or `fake-` (e.g., `FAKE_GMAPS_API_KEY`, `mock-oauth-token`) so `.gitleaks.toml` allowlists them automatically without per-file exceptions.
- **Install Pre-Commit Hook**: Configure Git to run the versioned pre-commit secret and PII check (`gitleaks git --pre-commit --staged --config .gitleaks.toml --redact --no-banner --log-level warn --verbose`):
  `./scripts/setup-hooks.sh`
- **Local Gitleaks Scans**:
  - Scan full git commit history of current HEAD (matches CI `secret-scan` job):
    `gitleaks git --log-opts="HEAD" --config .gitleaks.toml --redact`
  - Scan working tree files (including untracked files not ignored by `.gitignore`):
    `gitleaks dir --config .gitleaks.toml --redact`
  - Scan staged changes manually before committing:
    `gitleaks git --pre-commit --staged --config .gitleaks.toml --redact`

### Frontend Tests
- **Vitest**: Run single test files with `npm test path/to/test.test.tsx`, or run the full suite with capped workers matching CI (`npx vitest run --maxWorkers=2` or `npm test` in `web/`).
- **Implicit Vitest Imports**: Always import Vitest functions explicitly:
  `import { describe, it, expect, beforeEach, vi } from 'vitest';`
- **Mock Setup**: Update `web/src/test/apiMocks.ts` whenever shared settings or notification types are modified.
- **Production Build**: Run `npm run build` in `web/` (`cd web && npm run build`) to type-check and bundle `web/dist` (including `sw.js` and SEO HTML subpages) without `dist/stats.html`.
- **Bundle Analysis**: Run `ANALYZE=true npm run build` in `web/` (`cd web && ANALYZE=true npm run build`) to activate `vite-bundle-analyzer` and emit `dist/stats.html` on demand.

---

## Frontend & UI Conventions
- **Base-UI Components**:
  - Prefer `@base-ui/react/<component>` over custom controls or third-party UI libraries (do not assume Tailwind or Tremor are installed).
  - Review `web/llms.txt` for available Base-UI components, imports, and prop types.
  - Available Base-UI components:
    Accordion, Alert, Autocomplete, Avatar, Button, Checkbox, Collapsible, Combobox, Context, Dialog, Drawer, Field, Fieldset, Form, Input, Menu, Menubar, Meter, Navigation, Number, Popover, Preview, Progress, Radio, Scroll, Select, Separator, Slider, Switch, Tabs, Toast, Toggle, Toolbar, Tooltip.
- **Layout & CSS**:
  - Reuse global styles from `App.css` (e.g., `.content-container`, `.card`, `.btn`, `.form-group`, `.switch-row`).
  - Component-specific layout and spacing belong in a dedicated `<Component>.css` file. Avoid excessive inline styles.

---

## Operations & Site Debugging
- **GCP Project**: Cloud Logging and Firestore run under project `raterudder` (`projects/raterudder`).
- **Polling Interval**: RateRudder site updates (`/api/updateSites`) run every **20 minutes** (not every 5 minutes; do not confuse 5-minute utility price feeds or cache TTLs with the control loop interval).
- **Site Debugging Skill**: Use the `debug-site` skill (`cmd/inspectsite` and `cmd/replayplan`) to inspect site settings, action history, cloud logs, and re-run `controller.Plan` across historical polls.
- **Privacy Guardrail**: Never include site IDs, customer names, or user emails in git commits, tests, or code files. Before committing fixture updates or site debugging changes, install the pre-commit hook (`./scripts/setup-hooks.sh`), verify `gitleaks git --pre-commit --staged --config .gitleaks.toml --redact` and `gitleaks dir --config .gitleaks.toml --redact` exit `0`, and run `go test ./pkg/controller -run "^TestFixturePrivacy$"` (`cmd/fetchhistory` maps sites to synthetic `site<N>` IDs and rounds weather `latitude`/`longitude` to 2 decimal places).
- **Event Analytics Skill**: Use the `query-events` skill (`.agents/skills/query-events/SKILL.md`, event catalog in `pkg/server/events.go`) to query and aggregate structured `jsonPayload.event` logs in Cloud Logging.
- **Privacy Guardrail**: Never include site IDs, customer names, or user emails in git commits, tests, or code files.
