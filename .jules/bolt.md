## 2025-01-28 - Slice Capacity
**Learning:** When preallocating slices with `make([]Type, 0, capacity)` to avoid dynamic array resizing, if we are filtering a superset array down to a fixed window (e.g. 72 hours down to 24 hours), we should use the smaller, known capacity bound (24) rather than `len(source)` (72) to avoid over-allocating memory.
**Action:** When preallocating slices inside a filter loop, use the expected logical bound (like `24` for a 24-hour window) instead of `len(source)`.

## 2026-03-14 - Batch Site ID Validation
**Learning:** Sequential database queries (N+1 style) in high-traffic or user-facing paths like `handleJoin` introduce unnecessary latency and round-trip overhead. Even without a formal performance baseline, reducing $O(N)$ database calls to $O(1)$ by using existing bulk methods like `ListSites` and performing in-memory lookups is a significant architectural win.
**Action:** Always prefer bulk fetching and in-memory lookups over sequential `Get` calls when validating multiple candidate identifiers.
