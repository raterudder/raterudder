# Agent Guidelines & Project Standards

## Architecture & Project Layout
- `cmd/raterudder`: Main entry point and orchestration loop.
- `pkg/controller`: Battery charging/discharging decision logic and scheduling simulations.
- `pkg/utility`: Electricity tariff fetchers (e.g., ComEd hourly/real-time pricing).
- `pkg/ess`: Energy Storage System integrations (FranklinWH, Tesla, etc.).
- `pkg/storage`: Persistence layer using Google Cloud Firestore.
- `pkg/server`: HTTP API endpoints for notifications, settings, history, auth, and telemetry.
- `pkg/model` & `pkg/types`: Shared domain models, tariff structures, and interfaces.
- `web`: React frontend utilizing `@base-ui/react`.
- `deployment/tf`: Example Terraform deployment configurations.

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
- **Struct Comparisons with Slices**: In Go, structs containing slice fields (e.g., `UserNotificationSettings` with `QuietPeriods []TimePeriod`) cannot be compared with `==`. Use `reflect.DeepEqual(a, b)` in storage/persistence checks.

---

## Testing Guidelines

### Go Tests
- **Run Single Tests**: Run a single test at a time using regex:
  `go test ./pkg/server -run "^TestFunctionName$"`
  (Do not pass `-name`; `go test` uses `-run`).
- **Never Pass `-v`**: Avoid passing `-v` to `go test` to prevent noisy output.
- **Never Redirect Output**: Do not redirect test output to a file (no `> out.txt`).
- **Subtests with `t.Run`**:
  - Tests live in `<file>_test.go` named `Test<FunctionName>`.
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

### Frontend Tests
- **Vitest**: Run single test files with `npm test path/to/test.test.tsx`.
- **Implicit Vitest Imports**: Always import Vitest functions explicitly:
  `import { describe, it, expect, beforeEach, vi } from 'vitest';`
- **Mock Setup**: Update `web/src/test/apiMocks.ts` whenever shared settings or notification types are modified.

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
