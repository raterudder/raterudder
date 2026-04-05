## 2026-03-26 - Do not use mock.Anything in tests
**Learning:** Even when the specific argument value is not critical (like an email string that doesn't trigger specific mock behavior), using `mock.Anything` is an anti-pattern because it reduces assertion thoroughness and violates strict testing boundaries. The only exception to this rule is for `context.Context` arguments, where `mock.Anything` is acceptable since contexts are typically inconsequential to the test's outcome.
**Action:** Always use explicit values or specific matching strategies for parameters to ensure assertion thoroughness, but you may use `mock.Anything` for `context.Context` arguments.

## 2024-04-05 - Strict Mock Assertions with testify/mock
**Learning:** Overly broad mocks like `mock.Anything` can hide bugs where unexpected parameters are passed to dependencies. In history endpoints where dynamic data based on current vs past dates are used, ensuring tests use exact matches (like `mock.AnythingOfType("time.Time")` and verifying `Cache-Control` headers) improves confidence.
**Action:** Always replace `mock.Anything` with type-specific matchers or explicit parameters when the parameter value changes behavior, and suffix mock setup with `.Once()` to catch unexpected duplicate invocations. Always add assertions for `Cache-Control` max-age where appropriate.
