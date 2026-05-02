
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2026-05-02 - Information Leakage via Error Responses (CWE-209)
**Vulnerability:** Several API endpoints (`pkg/server/settings.go`, `pkg/server/tesla.go`) were returning raw error details from internal dependencies (like `fmt.Sprintf("...: %v", err)`) directly to clients in the JSON response body. This creates an information leakage vulnerability (CWE-209), potentially exposing internal system state, file paths, or dependency details to unauthorized actors.
**Learning:** Returning `err.Error()` or formatting underlying errors into client HTTP responses is a common anti-pattern. While useful for debugging, it breaks the principle of failing securely and can expose sensitive internal mechanics.
**Prevention:** Always log the complete, raw error context on the backend using `log.Ctx(ctx).ErrorContext(...)` for debugging observability, and return only a safe, generic, static string message to the client (e.g., `writeJSONError(w, "invalid utility provider settings", ...)`).
