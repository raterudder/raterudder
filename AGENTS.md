Tests
- go test ./folder/subfolder -name "TestFunctionName" -v
- npm test path/to/test.test.tsx

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
