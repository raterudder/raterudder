
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-18 - Prevent Error Leakage in JSON Responses
**Vulnerability:** Information leakage (CWE-209) via `writeJSONError` formatting actual error structures into responses in `pkg/server/tesla.go`, `pkg/server/settings.go`, `pkg/server/history.go`, and `pkg/server/savings.go`.
**Learning:** Returning `err.Error()` or `fmt.Sprintf("... %v", err)` directly to the user in HTTP API responses reveals internal backend details which could be used by an attacker to infer system architecture, underlying errors, or potential exploit paths.
**Prevention:** Always substitute backend error details with generic, static error messages when returning data to the client. Keep actual error details restricted to server-side logging (e.g. via `log.Ctx(ctx).ErrorContext(...)`).
