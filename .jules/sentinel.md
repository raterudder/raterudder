
## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-15 - IP Spoofing via CF-Connecting-IP
**Vulnerability:** The `getClientIP` function in `pkg/server/ratelimit.go` was extracting the client IP from the `CF-Connecting-IP` header before checking `X-Forwarded-For`. Because there were no checks to verify if the request actually originated from Cloudflare's IP ranges, an attacker could trivially bypass rate limits and IP restrictions by manually setting the `CF-Connecting-IP` header.
**Learning:** Blindly trusting vendor-specific HTTP headers for identifying client IPs without strict upstream validation is a dangerous pattern. If the application is ever exposed directly to the internet (or through a different proxy), attackers can completely spoof their origin.
**Prevention:** Always rely on standard, securely parsed headers like `X-Forwarded-For` (parsing in reverse to find the last public IP), or ensure that the infrastructure explicitly drops these headers from untrusted sources before they reach the application layer.
