## 2026-04-14 - [OIDC Email Verification Bypass]
**Vulnerability:** The application was vulnerable to an authorization bypass where an OIDC token completely missing the `email_verified` claim was trusted by default.
**Learning:** In Go, when unmarshaling dynamic JSON into a struct with an `any` type (like `EmailVerified any`), missing fields are `nil`. Defaulting authentication state to `true` when claims are missing creates a secure-by-default failure.
**Prevention:** Always default security booleans (like authentication or verification checks) to `false`. Explicitly set to `true` only when the exact claim is present and evaluates to true.
