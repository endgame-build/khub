---
type: requirement
title: Encrypt sensitive payloads at rest
---
Customer PII fields are sealed with envelope encryption before they reach the
order store. Data keys come from the KMS gateway and rotate yearly.
