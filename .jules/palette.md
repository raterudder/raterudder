## 2024-04-24 - Hidden Keyboard Shortcut Hints
**Learning:** Adding keyboard shortcuts to elements is great for power users, but always showing them can add visual clutter.
**Action:** When an interactive element has a hidden keyboard shortcut, append a `<kbd>` element (e.g., `<kbd aria-hidden="true">←</kbd>`) that becomes visible on hover or focus. This UX pattern improves discoverability for power users without adding permanent visual clutter to the UI.
