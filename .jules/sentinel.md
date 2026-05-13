
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-24 - Rate Limit Bypass via CF-Connecting-IP Spoofing
**Vulnerability:** The `getClientIP` function trusted the `CF-Connecting-IP` header unconditionally. Since the application isn't strictly behind Cloudflare, attackers could spoof this header to easily bypass rate limiting on sensitive endpoints (like login/register).
**Learning:** Trusting CDN-specific headers like `CF-Connecting-IP` or `X-Real-IP` without verifying the request actually originated from the CDN's trusted IP ranges creates a trivial IP spoofing vulnerability.
**Prevention:** Rely on reverse-parsing the standard `X-Forwarded-For` header to find the last public IP, which is harder to spoof when correctly configured by reverse proxies, or explicitly validate the remote address against trusted CDN IP ranges before honoring CDN-specific headers.
