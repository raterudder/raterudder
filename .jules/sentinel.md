
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-11 - IP Spoofing via CF-Connecting-IP
**Vulnerability:** The application blindly trusted the `CF-Connecting-IP` header without validating if the request came through Cloudflare, allowing malicious clients to easily spoof their IP address to bypass rate limiting.
**Learning:** Rate limiting based on easily spoofed headers completely undermines the protection mechanism. Always use headers that are reliably set by the closest trusted reverse proxy, or parse `X-Forwarded-For` from right to left to find the first untrusted IP.
**Prevention:** Only trust `CF-Connecting-IP` if you can verify the connection is from Cloudflare IP ranges. Otherwise, rely exclusively on reverse-parsing `X-Forwarded-For` or the connection `RemoteAddr`.
