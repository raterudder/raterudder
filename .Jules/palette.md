
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-04-11 - Hide decorative indicator dots from screen readers
**Learning:** Decorative color dot elements (`<span className="dot ..."></span>`) used in dashboard widgets (like SavingsHero) are often parsed by screen readers unnecessarily, even if empty, depending on how they are nested alongside visible labels.
**Action:** Always add `aria-hidden="true"` to purely visual CSS shapes (like indicator dots or color swatches) that are adjacent to actual text labels to reduce screen reader noise.
