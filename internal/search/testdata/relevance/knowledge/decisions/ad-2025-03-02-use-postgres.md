---
type: adr
title: Use Postgres for the order store
---
We store orders in Postgres. Row-level locks cover the booking race, and the
team already runs it.
