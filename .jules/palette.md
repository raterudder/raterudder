
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-05-24 - Hide decorative hamburger menu lines from screen readers
**Learning:** Purely visual, CSS-driven decorative elements like the spans used for a hamburger menu icon can clutter screen reader output if not explicitly hidden. Even if the parent button has a descriptive `aria-label`, the empty child spans might still be announced.
**Action:** Always add `aria-hidden="true"` to empty `<span>` tags that are used solely for decorative purposes, such as building icon lines via CSS.

## 2024-05-25 - Hide decorative toggle icons in Accordions
**Learning:** Purely visual, CSS-driven decorative elements like `<span className="toggle-icon" />` used to display a `+` or `-` state in Accordion headers can confuse screen readers if not hidden.
**Action:** Always add `aria-hidden="true"` to empty `<span>` tags that are used solely for decorative purposes, such as indicator dots or toggle icons.
## 2026-05-06 - Hide text emojis/entities from screen readers in icon-only buttons
**Learning:** Relying purely on an `aria-label` for an icon-only button is not enough to stop a screen reader from attempting to read inner text-based icons (like the `&times;` HTML entity or emoji characters like `💬`). The screen reader will announce the label followed by a potentially confusing or redundant reading of the character.
**Action:** When implementing icon-only buttons that use text or emoji as the visual icon instead of SVG, wrap the text or emoji in a `<span aria-hidden="true">`. This ensures the screen reader only reads the descriptive `aria-label` on the parent button.
