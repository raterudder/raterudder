
## 2025-04-25 - Add Keyboard Shortcut Hints to DateSelector
**Learning:** Hidden keyboard shortcuts (like ArrowLeft/ArrowRight for date navigation) can remain undiscoverable unless explicitly surfaced in the UI. For power users, revealing these shortcuts on hover or focus reduces friction and cognitive load.
**Action:** When implementing keyboard shortcuts for interactive elements, append a `<kbd aria-hidden="true">` hint element inside the component that is visually hidden by default and becomes visible on hover/focus using CSS `opacity`. This ensures discoverability without adding permanent visual clutter.
