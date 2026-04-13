
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-05-18 - Avoid Duplicating Global Focus Styles
**Learning:** The frontend application defines global accessibility focus styles (`:focus-visible`) in `web/src/App.css` for standard interactive elements. Re-implementing these in component-specific CSS can lead to inconsistencies and duplicated efforts.
**Action:** When creating new components or modifying existing ones, rely on the global `:focus-visible` styles rather than creating custom focus outlines, unless a component specifically requires a unique focus state treatment.
