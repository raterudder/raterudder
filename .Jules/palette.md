
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-05-24 - Purely Decorative Emojis Need ARIA Hiding
**Learning:** React elements containing purely decorative text or emojis (e.g., `<div className="icon">⚡</div>`) that aren't intended to be read by screen readers will still be announced by default, cluttering the auditory experience.
**Action:** When adding purely decorative elements like CSS-styled indicator dots or emojis, always add `aria-hidden="true"` directly to the element to explicitly hide it from accessibility trees.
