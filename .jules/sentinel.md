## 2026-03-07 - Secure Handling of MD5 Protocol Requirements
 **Learning:** The FranklinWH API mandates MD5-hashed passwords for authentication. Storing these hashes directly in the database is insecure because MD5 is cryptographically weak.
 **Action:** Store the raw password instead (which is AES-GCM encrypted at the storage layer) and perform the MD5 hashing only on-the-fly during the authentication request. This keeps the weak hash out of the persistence layer while meeting the external API's protocol requirements.
