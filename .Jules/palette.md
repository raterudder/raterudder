## 2026-03-24 - Do not duplicate visible text in aria-labels
**Learning:** Adding an `aria-label` to a button that already contains the identical visible text (e.g., `<button aria-label="Log Out">Log Out</button>`) is an accessibility anti-pattern. It creates redundant announcements for screen readers and provides no additional value.
**Action:** Only use `aria-label` for icon-only buttons or when the visible text is insufficient to describe the button's purpose.

## 2026-03-24 - Async Button Feedback Pattern
**Learning:** The application uses a consistent pattern of displaying a loading spinner *inside* buttons during async operations, using the `<span className="loading-spinner" aria-hidden="true"></span>` element. This prevents the UI from shifting unexpectedly and clearly associates the loading state with the action taken.
**Action:** Always wrap async submit handlers with state (e.g., `isSubmitting`) and conditionally render the `.loading-spinner` inside the corresponding action button, applying `aria-hidden="true"` so it is ignored by screen readers, while updating the button text (e.g., 'Submitting...') for screen reader clarity.
## 2024-04-08 - Visualizing Loading States on Click in Playwright
**Learning:** Testing component behavior that is hidden behind un-mockable delays or instantly resolving fetches in Playwright verification scripts can cause components (like loading spinners) to instantly unmount before a screenshot is taken.
**Action:** When trying to take a screenshot of a transient state like a submit button loading spinner, mock the API route to fulfill with a long delay using Python's `time.sleep` instead of instantly, and evaluate the click using JS (`evaluate("el => el.click()")`) rather than Playwright's native `click()`, then `expect(spinner).to_be_visible()` to catch the component before it finishes.
