
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-05-24 - Hide decorative hamburger menu lines from screen readers
**Learning:** Purely visual, CSS-driven decorative elements like the spans used for a hamburger menu icon can clutter screen reader output if not explicitly hidden. Even if the parent button has a descriptive `aria-label`, the empty child spans might still be announced.
**Action:** Always add `aria-hidden="true"` to empty `<span>` tags that are used solely for decorative purposes, such as building icon lines via CSS.

## 2024-05-25 - Hide decorative toggle icons in Accordions
**Learning:** Purely visual, CSS-driven decorative elements like `<span className="toggle-icon" />` used to display a `+` or `-` state in Accordion headers can confuse screen readers if not hidden.
**Action:** Always add `aria-hidden="true"` to empty `<span>` tags that are used solely for decorative purposes, such as indicator dots or toggle icons.
## 2024-05-26 - Hide standalone emojis and html entities in icon-only buttons
**Learning:** Even when icon-only buttons have an `aria-label`, screen readers may still announce the visible text content (like a "💬" emoji or an "&times;" symbol) inside them, leading to redundant or confusing announcements like "Feedback, speech balloon".
**Action:** Always wrap text-based icons, HTML entities, or emojis in a `<span aria-hidden="true">` when they are placed inside a button that already provides its accessible name via `aria-label`.

## 2024-05-14 - Add ARIA Roles to Dynamic Error/Success Messages
**Learning:** When dynamically rendering error or success messages in the UI (e.g., `{error && <div>{error}</div>}`), they are visually apparent but not always conveyed to assistive technologies unless focus is shifted to them.
**Action:** Always include `role="alert"` for dynamic error messages and `role="status"` for dynamic success messages to ensure screen readers announce them automatically without requiring a focus change.
