---
type: component
title: Auth service
---
Owns login, sessions and tokens. Failed attempts are counted in Redis so the
limiter holds across instances.
