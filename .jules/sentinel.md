
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-14 - IP Spoofing via CF-Connecting-IP Header
**Vulnerability:** The rate limiter trusted the `CF-Connecting-IP` header without verifying if the request actually originated from Cloudflare. An attacker could trivially bypass rate limits by spoofing this header.
**Learning:** Never trust client-provided headers for IP identification unless the application is strictly enforcing that the request came through a trusted proxy (like Cloudflare) and dropping direct connections.
**Prevention:** Rely on reverse-parsing `X-Forwarded-For` to find the last public IP, as the last proxy added by the infrastructure will append the true IP.
