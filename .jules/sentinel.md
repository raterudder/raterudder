
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2026-04-23 - [Information Leakage in API Error Responses]
**Vulnerability:** Internal backend errors (like database errors or upstream authentication failures) were being returned directly to the client via `writeJSONError` using `err.Error()` or `fmt.Sprintf("%v", err)`. This exposes raw internal details, creating an information leakage vulnerability (CWE-209).
**Learning:** Returning internal errors to the user gives potential attackers insight into backend infrastructure, package paths, and database queries.
**Prevention:** Always log the original error server-side using `log.Ctx().ErrorContext()` and return a generic, static error string to the client. Only user input validation errors are safe to return to the client.
