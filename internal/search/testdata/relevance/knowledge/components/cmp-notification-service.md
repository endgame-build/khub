---
type: component
title: Notification service
---
Sends email, SMS and push. Each provider has its own rate limit, so the
service keeps a rate budget per provider, backs off when a provider rate
limits it, and reports the send rate, the bounce rate and the complaint rate
per hour. When the rate budget is spent, messages wait in the queue rather
than fail.
