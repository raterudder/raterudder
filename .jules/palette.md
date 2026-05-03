## 2024-05-01 - Add keyboard shortcut hints

**Learning:** Interactive elements with hidden keyboard shortcuts (like arrow keys for date navigation) can have improved discoverability by adding a `<kbd>` element that becomes visible on hover or focus.
**Action:** When an interactive element has a hidden keyboard shortcut, append a `<kbd>` element (e.g., `<kbd aria-hidden="true">←</kbd>`) that becomes visible on hover or focus. This UX pattern improves discoverability for power users without adding permanent visual clutter to the UI.
## 2026-05-03 - Adding Tooltips to Dynamically Disabled Primary Action Buttons
**Learning:** When primary action buttons (like form submit buttons) are dynamically disabled due to incomplete data, users can be confused as to what is missing to enable them, especially in large or complex forms.
**Action:** Improve UX by adding a `title` tooltip to dynamically disabled primary action buttons that explains exactly why the button is disabled (e.g., "Please fill out all fields to join"). This natively provides helpful context without cluttering the UI.
