
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.
## 2024-05-01 - Prevent Error Leakage in API Responses
**Vulnerability:** HTTP handlers were returning raw, unhandled internal errors (e.g., `err.Error()`, `fmt.Sprintf("...%v", err)`) directly to the client via `writeJSONError`.
**Learning:** This exposes underlying implementation details, infrastructure states, or stack traces, leading to Information Leakage (CWE-209).
**Prevention:** Always log the raw internal error using context-aware logging (`log.Ctx(ctx).ErrorContext(...)`) and return a sanitized, static error string to the client. Update associated tests that rely on the raw string.
