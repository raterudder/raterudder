## 2025-05-14 - Direct Type Assertions for Debugging
**Learning:** In some cases, direct type assertions like `ctx.Value(key).(string)` are preferred over safe assertions. This causes a panic if the value is missing, which provides a stack trace that is crucial for debugging when a context value is mandatory for the application's correct operation.
**Action:** Use direct type assertions when a value's absence indicates a critical developer error that should be surfaced immediately with a stack trace.
## 2026-03-26 - Do not use mock.Anything in tests
**Learning:** Even when the specific argument value is not critical (like a generic context or an email string that doesn't trigger specific mock behavior), using `mock.Anything` is an anti-pattern because it reduces assertion thoroughness and violates strict testing boundaries. Code review will flag this as a non-blocking nitpick or partial failure.
**Action:** Always use specific matching strategies like `mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })` for contexts or explicit values for other arguments instead of `mock.Anything`.
