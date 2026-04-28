
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-15 - [Prevent Information Leakage in API Responses]
**Vulnerability:** Raw backend error messages were directly returned in API JSON error responses via `writeJSONError(w, err.Error(), ...)`.
**Learning:** Returning raw Go error strings directly exposes internal implementation details (such as API call structures, storage constraints, or internal service state) to the client, creating a CWE-209 (Generation of Error Message Containing Sensitive Information) vulnerability.
**Prevention:** Instead of directly returning `err.Error()`, API handlers should always mask internal errors using a generic, client-friendly message (e.g., "failed to register tesla") in the HTTP response, and simultaneously log the actual underlying error securely on the server side using the structured logger to maintain observability.
