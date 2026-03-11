## 2025-03-08 - Prevent Rate Limit Bypass on ESS Login (Update Settings)
**Vulnerability:** A logic flaw allowed attackers to bypass exponential backoff and brute-force external ESS systems. When users submitted changed credentials, the `ConsecutiveFailures` counter was decreased and `LastAttempt` reset, meaning attackers could continually cycle incorrect passwords and bypass the lockout.
**Learning:** Tracking rate-limits directly via a persistent storage field requires strict monotonic growth upon failure. Any mechanic that reduces failures on arbitrary user input (like a "new" password attempt) fundamentally breaks the rate limiter.
**Prevention:** Never reset or decrease a failure tracking counter based on the contents of the failed payload; only reset it on an authenticated/successful login.

## 2025-03-08 - Prevent Permanent Lockouts During Rate-Limiting
**Vulnerability:** A hard limit logic block permanently locked users out of updating their credentials on the settings endpoint if they failed 5 times, effectively causing a DoS.
**Learning:** When fixing rate-limiter bypasses, be careful not to introduce permanent user lockouts. A hard limit (e.g. blocking all attempts forever if failures >= 5) in a UI settings endpoint will prevent users from ever fixing their typos. Relying entirely on a time-based backoff allows users to eventually correct their input while securely throttling attackers.
**Prevention:** Let the exponential/linear time backoff handle brute-force prevention naturally. Avoid permanent lockout conditions on user-facing credential update endpoints unless there is an out-of-band recovery process.

## 2025-03-08 - Prevent Integer Overflow Bypass in Rate Limit Backoff
**Vulnerability:** The exponential backoff algorithm `getESSBackoff` used a bitwise left shift (`1 << (failures - 2)`) without capping the `failures` exponent. An attacker failing authentication 66 times or more caused an integer overflow, resulting in a 0s or negative backoff duration, completely bypassing the 15-minute maximum rate limit.
**Learning:** Unbounded user-driven integers used in bitwise operations for security logic (like backoff exponents) are highly susceptible to integer overflow, leading to logic bypasses.
**Prevention:** Always cap the exponent or failure counter *before* performing bitwise math to calculate backoffs.
