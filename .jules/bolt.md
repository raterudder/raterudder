## 2025-01-28 - Slice Capacity
**Learning:** When preallocating slices with `make([]Type, 0, capacity)` to avoid dynamic array resizing, if we are filtering a superset array down to a fixed window (e.g. 72 hours down to 24 hours), we should use the smaller, known capacity bound (24) rather than `len(source)` (72) to avoid over-allocating memory.
**Action:** When preallocating slices inside a filter loop, use the expected logical bound (like `24` for a 24-hour window) instead of `len(source)`.

## 2025-01-28 - Find Outliers in O(n)
**Learning:** When trying to find an outlier in a small or large array where a value is greater than the other values by a threshold, avoid O(n²) nested loop iterations.
**Action:** Optimize to O(n) by doing a single pass to find the top two maximum values and then comparing against those two maximums instead of all other elements in a loop.
