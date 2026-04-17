
## 2024-04-10 - Consistent Full-Page Loading States
**Learning:** React frontend components sometimes fallback to plain text loading indicators (e.g., `<div>Loading...</div>`) during asynchronous operations instead of using the consistent, styled visual loading state with the spinner.
**Action:** When working on UX for React components, identify plain text loading screens and replace them with the `<div className="loading-screen"><span className="loading-spinner loading-spinner-large" aria-hidden="true"></span>...</div>` pattern. Always remember to use `aria-hidden="true"` on the spinner to ensure screen readers focus on the semantic text.
## 2024-04-17 - Form Label Association
**Learning:** React form inputs without programmatic label association (using `htmlFor` and `id`) create accessibility barriers.
**Action:** When creating forms, always link `<label>` tags to their corresponding `<input>` or `<textarea>` tags by applying `htmlFor` on the label and matching it with the `id` on the input element, even if the label text is visually adjacent to the input.
