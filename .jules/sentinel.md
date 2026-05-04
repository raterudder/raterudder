
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-18 - CWE-209 Information Leakage in API Error Responses
**Vulnerability:** API endpoints were returning raw error strings via `err.Error()` and `fmt.Sprintf(..., err)` to clients, exposing internal implementation details and creating an information leakage vulnerability (CWE-209).
**Learning:** Returning internal errors to clients exposes the system's underlying structure and state. To avoid security theater, input validation errors are acceptable to display, but backend and system errors must be sanitized. Always ensure the original error is explicitly logged server-side to maintain observability before returning a generic response.
**Prevention:** Avoid passing raw `err` objects to response writers. Use generic client-friendly error messages and log the detailed error server-side.
