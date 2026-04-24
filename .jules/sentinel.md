
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-04-24 - Error Message Leakage in handleTeslaRegister
**Vulnerability:** In `pkg/server/tesla.go`, the raw error string from `s.ess.RegisterTesla(r.Context(), domain)` was being returned directly to the client via `writeJSONError(w, err.Error(), http.StatusInternalServerError)`, potentially exposing sensitive internal details (CWE-209).
**Learning:** Returning raw Go error strings directly to HTTP clients is an anti-pattern that can lead to information leakage. Internal errors should always be masked with generic, client-friendly messages.
**Prevention:** Always use generic messages (e.g., "internal server error") for 5xx responses. Ensure the original error is explicitly logged on the server side using `log.Ctx(ctx).ErrorContext(...)` to maintain observability without compromising security.
