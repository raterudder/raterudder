## 2026-03-07 - Secure Handling of MD5 Protocol Requirements
**Learning:** The FranklinWH API mandates MD5-hashed passwords for authentication. Storing these hashes directly in the database is insecure because MD5 is cryptographically weak.
**Action:** Store the raw password instead (which is AES-GCM encrypted at the storage layer) and perform the MD5 hashing only on-the-fly during the authentication request. This keeps the weak hash out of the persistence layer while meeting the external API's protocol requirements.
## 2026-03-06 - [CSP Configuration for Third-Party Auth]
**Vulnerability:** Missing Content-Security-Policy allowed potential Cross-Site Scripting (XSS) and data injection.
**Learning:** When implementing CSP, explicitly whitelisting third-party auth providers (like Google Identity Services and Apple Sign-In) in directives such as `script-src`, `style-src`, `connect-src`, and `frame-src` is strictly required. Otherwise, the authentication UI will be broken, rendering users unable to log in.
**Prevention:** Always verify external dependencies, especially authentication modules, before deploying a strict CSP, and use `report-to` alongside `Content-Security-Policy-Report-Only` (initially) to find required sources.
