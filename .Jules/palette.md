## 2026-03-24 - Do not duplicate visible text in aria-labels
**Learning:** Adding an `aria-label` to a button that already contains the identical visible text (e.g., `<button aria-label="Log Out">Log Out</button>`) is an accessibility anti-pattern. It creates redundant announcements for screen readers and provides no additional value.
**Action:** Only use `aria-label` for icon-only buttons or when the visible text is insufficient to describe the button's purpose.

## 2026-03-24 - Async Button Feedback Pattern
**Learning:** The application uses a consistent pattern of displaying a loading spinner *inside* buttons during async operations, using the `<span className="loading-spinner" aria-hidden="true"></span>` element. This prevents the UI from shifting unexpectedly and clearly associates the loading state with the action taken.
**Action:** Always wrap async submit handlers with state (e.g., `isSubmitting`) and conditionally render the `.loading-spinner` inside the corresponding action button, applying `aria-hidden="true"` so it is ignored by screen readers, while updating the button text (e.g., 'Submitting...') for screen reader clarity.
