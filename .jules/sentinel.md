
## 2026-04-16 - [CRITICAL] Fix unverified OIDC token bypass
**Vulnerability:** OIDC token validation allowed authentication bypass when the `email_verified` claim was omitted.
**Learning:** OIDC tokens omitting the `email_verified` claim must be explicitly treated as unverified to prevent authentication bypass vulnerabilities by malicious or misconfigured identity providers.
**Prevention:** Explicitly treat missing verification claims as unverified (`verified := false` instead of `true`).
