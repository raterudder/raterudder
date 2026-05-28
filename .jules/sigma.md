## 2026-03-26 - Do not use mock.Anything in tests
**Learning:** Even when the specific argument value is not critical (like an email string that doesn't trigger specific mock behavior), using `mock.Anything` is an anti-pattern because it reduces assertion thoroughness and violates strict testing boundaries. The only exception to this rule is for `context.Context` arguments, where `mock.Anything` is acceptable since contexts are typically inconsequential to the test's outcome.
**Action:** Always use explicit values or specific matching strategies for parameters to ensure assertion thoroughness, but you may use `mock.Anything` for `context.Context` arguments.
## 2026-04-10 - mock.AnythingOfType is redundant for statically typed languages
**Learning:** In statically typed languages like Go, using `mock.AnythingOfType("time.Time")` provides little value over `mock.Anything` because the compiler already enforces the type. It's better to use explicit expected values or `mock.MatchedBy` for assertions to actually test logic.
**Action:** When replacing `mock.Anything`, avoid falling back to `mock.AnythingOfType` for strong-typed parameters. Instead, calculate and provide the exact expected value, or use `mock.MatchedBy` to assert specific logical properties of the argument.
## 2026-04-14 - Populate TSDayStart when mocking GetEnergyHistory
**Learning:** When mocking `GetEnergyHistory` in server tests (like `pkg/server/history_test.go`), you must populate the `TSDayStart` field in the returned `DailyEnergyStats`. If it defaults to a zero value, any subsequent calculations based on that timezone (like `GetWeather` start/end times) will be wildly inaccurate and break tests when asserting exact parameter values instead of `mock.AnythingOfType`.
**Action:** Always populate `TSDayStart` explicitly (usually matching the requested `start` or `todayUTC`) in `DailyEnergyStats` mocks if downstream code loops over them to calculate offsets.
## 2026-04-18 - Ensure MatchedBy actually validates state
**Learning:** When replacing `mock.Anything` or `mock.AnythingOfType` with `mock.MatchedBy` to increase assertion strictness, the matching function must actively validate specific field values. Returning `true` unconditionally is an anti-pattern that defeats the purpose of the improvement and will fail code review.
**Action:** Always write `MatchedBy` functions that assert specific properties (e.g., `return s.UtilityProvider == "test"`) to genuinely test the logic.
## 2026-04-24 - Avoiding assumptions about truncated terminal output
**Learning:** The outputs of tools like `grep` and `cat` can be truncated by the terminal or sandbox. Assuming the contents of code based on a truncated response leads to planning invalid file modifications and failing the Groundedness Rule.
**Action:** Always fetch and read the full, un-truncated text (using tools like `sed` or targeted `cat` blocks) to verify the actual codebase state before proposing edits based on partial context.

## 2026-04-24 - Go package compilation error from scratchpad files
**Learning:** Creating a test file with a mismatched package name (like `package test` in a directory full of `package main` or `package server`) will instantly cause a "found multiple packages in the same directory" compilation error in Go, preventing test execution.
**Action:** When creating new Go files, even if they are temporary or just used as a scratchpad, always match the package declaration of the existing files in that directory, or just delete the file immediately when done.

## 2024-05-28 - Avoid global find-and-replace for context matchers
**Learning:** When replacing `mock.Anything` with a stricter context matcher (`mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })`) in Go tests, global replacements or overly broad regular expressions can inadvertently modify `mock.Anything` usages intended for non-context parameters (like strings or error returns), causing type mismatch panics in the test suite.
**Action:** Use targeted, explicitly validated string replacements (e.g., via a Python script calling `content.replace('mockW.On("Location", mock.Anything', '...')`) that have been pre-confirmed via `grep` to only modify the intended context arguments.
