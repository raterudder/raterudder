
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.
## 2024-05-16 - Removed CF-Connecting-IP Header Trust
**Vulnerability:** IP Spoofing (CWE-348) by blindly trusting the `CF-Connecting-IP` header in `getClientIP`.
**Learning:** Checking proxy headers without validating the source IP range allows attackers to forge their IP, bypassing rate limiting.
**Prevention:** Only trust `X-Forwarded-For` with reverse parsing for the last public IP, or explicitly validate that requests originate from trusted Cloudflare IP ranges before honoring `CF-Connecting-IP`.
