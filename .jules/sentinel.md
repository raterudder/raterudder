
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.
## 2024-05-18 - [Fix IP Spoofing in Rate Limiting]
**Vulnerability:** Trusting `CF-Connecting-IP` header for IP identification allows IP spoofing and rate limit bypass unless explicitly restricted to Cloudflare proxies.
**Learning:** Always validate that headers used for identity (like IP) cannot be trivially spoofed by a client connecting directly or through an untrusted intermediary.
**Prevention:** Use `X-Forwarded-For` with reverse parsing to find the last public IP, assuming the direct connection is untrusted unless specifically proven otherwise.
