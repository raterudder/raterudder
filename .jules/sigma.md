## 2025-05-14 - Direct Type Assertions for Debugging
**Learning:** In some cases, direct type assertions like `ctx.Value(key).(string)` are preferred over safe assertions. This causes a panic if the value is missing, which provides a stack trace that is crucial for debugging when a context value is mandatory for the application's correct operation.
**Action:** Use direct type assertions when a value's absence indicates a critical developer error that should be surfaced immediately with a stack trace.
