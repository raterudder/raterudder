## 2024-05-01 - Add keyboard shortcut hints

**Learning:** Interactive elements with hidden keyboard shortcuts (like arrow keys for date navigation) can have improved discoverability by adding a `<kbd>` element that becomes visible on hover or focus.
**Action:** When an interactive element has a hidden keyboard shortcut, append a `<kbd>` element (e.g., `<kbd aria-hidden="true">←</kbd>`) that becomes visible on hover or focus. This UX pattern improves discoverability for power users without adding permanent visual clutter to the UI.
