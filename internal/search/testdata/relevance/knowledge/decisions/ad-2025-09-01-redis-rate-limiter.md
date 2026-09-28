---
type: adr
title: Use Redis for the login limiter
---
The login limiter counts attempts in Redis with a sliding window. Postgres was
too slow under a credential-stuffing burst.
