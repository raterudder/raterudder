
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-18 - Information Leakage via Raw Error Responses
**Vulnerability:** Returning raw backend error messages (e.g., `err.Error()`) to clients in `pkg/server/tesla.go` exposed internal system details, creating an information leakage vulnerability (CWE-209).
**Learning:** Directly outputting backend errors can reveal infrastructure details, authentication strategies, or stack traces to potentially malicious users.
**Prevention:** Always sanitize API error responses by providing generic, client-friendly error strings and explicitly log the original underlying error on the server side using the context logger to maintain debug observability.
