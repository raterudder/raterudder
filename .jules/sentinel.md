## 2024-04-18 - Authentication Bypass due to OIDC Claim Defaulting
**Vulnerability:** In `pkg/server/auth.go`, the `email_verified` claim was defaulting to `true` when it was missing from the token. This created a high-severity authentication bypass vulnerability where a malicious or misconfigured OIDC identity provider omitting the claim would automatically be treated as verified.
**Learning:** Never default to `true` or a permissive state when handling authentication or authorization claims. Secure defaults (fail-closed) are crucial when interacting with external identity providers.
**Prevention:** Always assume claims are unverified (`verified := false`) unless explicitly present and valid in the token. Implement thorough unit tests that verify behavior when claims are missing entirely, not just when they are present and invalid.

## 2024-05-12 - IP Spoofing via CF-Connecting-IP
**Vulnerability:** In `pkg/server/ratelimit.go`, the `getClientIP` function trusts the `CF-Connecting-IP` header over other sources to determine the client's IP address. This header can be easily spoofed by an attacker if the application is not actually behind Cloudflare.
**Learning:** Never trust client-provided headers like `CF-Connecting-IP` blindly.
**Prevention:** Rely on the reverse-parsed `X-Forwarded-For` to find the last public IP.
