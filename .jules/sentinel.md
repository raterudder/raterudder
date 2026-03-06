## 2026-03-06 - [CSP Configuration for Third-Party Auth]
**Vulnerability:** Missing Content-Security-Policy allowed potential Cross-Site Scripting (XSS) and data injection.
**Learning:** When implementing CSP, explicitly whitelisting third-party auth providers (like Google Identity Services and Apple Sign-In) in directives such as `script-src`, `style-src`, `connect-src`, and `frame-src` is strictly required. Otherwise, the authentication UI will be broken, rendering users unable to log in.
**Prevention:** Always verify external dependencies, especially authentication modules, before deploying a strict CSP, and use `report-to` alongside `Content-Security-Policy-Report-Only` (initially) to find required sources.
