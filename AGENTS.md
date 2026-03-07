Tests
- go test ./folder/subfolder -name "TestFunctionName" -v
- npm test path/to/test.test.tsx
- Start the Firestore emulator for tests: `npx firebase-tools emulators:start --only firestore --project demo-test`
- When testing Go HTTP handlers that use `authMiddleware`, `bypassAuth` is usually false. Use `setupOIDCTest(t)` and `generateTestToken` to create mock OIDC servers and valid JWT tokens, passing them as an `authTokenCookie` in the `httptest.NewRequest`. Do not rely solely on injecting `context.WithValue` as the middleware may overwrite it.


Architecture
- cmd/raterudder is the main entry point and orchestrator.
- pkg/controller contains decision logic for charging/discharging.
- pkg/utility fetches electricity prices (ComEd).
- pkg/ess controls the Energy Storage System (FranklinWH).
- pkg/storage persists data/config using Google Cloud Firestore.
- pkg/server exposes HTTP API endpoints for update/history.
- pkg/model and pkg/types hold shared data models and types.
- web is the React frontend.
- tf is the actual Terraform configuration for deployment.
- deployment/tf is an example Terraform configuration for deployment.

Code Style
- Use go fmt formatting and standard Go import grouping.
- Keep package boundaries: controller logic in pkg/controller, external IO in utility/ess/storage/server.
- Prefer context.Context in public APIs and return (value, error).
- Use testify assert/require in tests.
- Log with slog and use slog.String, slog.Int, slog.Float64, slog.Bool, slog.Any, etc for key value pairs
- Log field names with headless camelcase
- Struct fields should have json Go tags with their name headless camelcase
- Tests should live in a file with the same name as the file they test, with suffix _test
- Tests should be named Test<FunctionName> and all related tests should live as subtests in that function with t.Run()
- Use assert.ErrorContains for error messages
- Use require.NoError to require errors didn't happen when you don't expect them
- Review web/llms.txt for available base-ui components and their props
- When using assert methods from testify, put them in if statements and wrap dependent code on that assert passing. Like if assert.Len(slice, 2) { assert.Equal(slice[0], "expected") }
- Prefer base-ui components over any custom components. Ensure you check for correct exports from `@base-ui/react` and don't assume external UI libraries (like Tremor or Tailwind CSS) are installed.
- Reuse existing global layout classes and styles from `App.css` (e.g., `.content-container`, `.card`, `.btn`), but otherwise store component/page specific layout and spacing styles in a component-specific `.css` file. Avoid excessive inline styles or non-existent utility classes.
- Make sure to import vitest test functions implicitly like `import { describe, it, expect, beforeEach, vi } from 'vitest';` in frontend tests.

Base-ui Components:
Accordion
Alert
Autocomplete
Avatar
Button
Checkbox
Checkbox
Collapsible
Combobox
Context
Dialog
Drawer
Field
Fieldset
Form
Input
Menu
Menubar
Meter
Navigation
Number
Popover
Preview
Progress
Radio
Scroll
Select
Separator
Slider
Switch
Tabs
Toast
Toggle
Toggle
Toolbar
Tooltip
