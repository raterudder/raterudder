
## 2026-04-07 - Hide decorative characters and emojis from screen readers
**Learning:** Decorative text characters (like `<` or `>`) and emojis adjacent to visible text labels are read aloud by screen readers, causing redundancy and confusion for visually impaired users.
**Action:** When adding visual flourishes or decorative icons/characters to UI components next to descriptive text, wrap them in `<span aria-hidden="true">` to improve the accessibility experience.
