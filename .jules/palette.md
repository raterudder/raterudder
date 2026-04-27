## 2024-04-27 - Keyboard Shortcuts for Interactive Elements
**Learning:** When an interactive element has a hidden keyboard shortcut, append a `<kbd>` element that becomes visible on hover or focus. This UX pattern improves discoverability for power users without adding permanent visual clutter to the UI.
**Action:** Use CSS transitions on the `max-width` and `opacity` properties of a hidden `<kbd>` element to create a smooth reveal animation when the parent element is hovered or focused.
