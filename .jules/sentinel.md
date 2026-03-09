## 2025-03-08 - Prevent Rate Limit Bypass on ESS Login (Update Settings)
**Vulnerability:** A logic flaw allowed attackers to bypass exponential backoff and brute-force external ESS systems. When users submitted changed credentials, the `ConsecutiveFailures` counter was decreased and `LastAttempt` reset, meaning attackers could continually cycle incorrect passwords and bypass the lockout.
**Learning:** Tracking rate-limits directly via a persistent storage field requires strict monotonic growth upon failure. Any mechanic that reduces failures on arbitrary user input (like a "new" password attempt) fundamentally breaks the rate limiter.
**Prevention:** Never reset or decrease a failure tracking counter based on the contents of the failed payload; only reset it on an authenticated/successful login.

## 2025-03-08 - Prevent Permanent Lockouts During Rate-Limiting
**Vulnerability:** A hard limit logic block permanently locked users out of updating their credentials on the settings endpoint if they failed 5 times, effectively causing a DoS.
**Learning:** When fixing rate-limiter bypasses, be careful not to introduce permanent user lockouts. A hard limit (e.g. blocking all attempts forever if failures >= 5) in a UI settings endpoint will prevent users from ever fixing their typos. Relying entirely on a time-based backoff allows users to eventually correct their input while securely throttling attackers.
**Prevention:** Let the exponential/linear time backoff handle brute-force prevention naturally. Avoid permanent lockout conditions on user-facing credential update endpoints unless there is an out-of-band recovery process.
