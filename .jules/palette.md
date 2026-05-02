## 2024-05-01 - Add keyboard shortcut hints

**Learning:** Interactive elements with hidden keyboard shortcuts (like arrow keys for date navigation) can have improved discoverability by adding a `<kbd>` element that becomes visible on hover or focus.
**Action:** When an interactive element has a hidden keyboard shortcut, append a `<kbd>` element (e.g., `<kbd aria-hidden="true">←</kbd>`) that becomes visible on hover or focus. This UX pattern improves discoverability for power users without adding permanent visual clutter to the UI.
## 2024-05-02 - Add tooltip explaining disabled button state

**Learning:** When primary action buttons (like submit buttons) are disabled due to incomplete form data, users may be confused about what action they need to take to enable the button.
**Action:** Add a `title` tooltip to disabled buttons explaining exactly why they are disabled (e.g., `title={!name.trim() ? "Please enter a site name" : undefined}`). This provides immediate, context-aware help without cluttering the UI.
