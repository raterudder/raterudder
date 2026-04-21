
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.
## 2024-05-24 - Rate Limiting IP Spoofing
**Vulnerability:** The rate limiting middleware (`pkg/server/ratelimit.go`) trusted the `CF-Connecting-IP` and `X-Forwarded-For` headers unconditionally without verifying if the request actually came from a trusted proxy.
**Learning:** This allowed an attacker to easily spoof their IP address by injecting these headers, bypassing the IP-based rate limits entirely (which are crucial for sensitive endpoints like login).
**Prevention:** Always verify that the `RemoteAddr` belongs to a trusted proxy (e.g., local network, loopback, or a known list of proxy IPs) before trusting the contents of proxy headers like `X-Forwarded-For` or `CF-Connecting-IP`.
