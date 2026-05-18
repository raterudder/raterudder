
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-18 - Rate Limiting IP Spoofing via Unverified Headers
**Vulnerability:** The rate limiter's `getClientIP` function blindly trusted the `CF-Connecting-IP` header.
**Learning:** External headers like `CF-Connecting-IP` can be trivially spoofed by attackers unless the application is strictly firewalled to only accept traffic from a specific CDN. Trusting it indiscriminately allows attackers to bypass rate limits by rotating the header value.
**Prevention:** Rely on reverse-parsing `X-Forwarded-For` to find the last public IP, or use `RemoteAddr`. Never trust unverified client-provided IP headers.
