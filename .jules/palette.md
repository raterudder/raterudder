
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
## 2024-05-31 - Group interactive elements and announce dynamic changes
**Learning:** For components with interactive elements that update dynamically displayed text (like `DateSelector` where "Next"/"Prev" buttons change the displayed date), grouping the controls and making the text a polite live region significantly improves the screen reader experience. Otherwise, screen reader users might not know the content updated without manually moving focus.
**Action:** When creating components with controls that update non-interactive text, add `role="group"` and `aria-label="..."` to the parent container to link them contextually. Add `aria-live="polite"` and `aria-atomic="true"` to the element containing the dynamically updating text.
