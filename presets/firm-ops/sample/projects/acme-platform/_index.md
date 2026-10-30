---
type: project
created: 2026-10-28
updated: 2026-10-28
draft: false
title: Acme Platform
external_repo: acme/platform
client: acme
owner: noor-haddad
team:
- ben-okafor
origin_opportunity: acme-platform-rebuild
depends_on:
- acme-data-pipeline
---
Rebuild of Acme's order platform. Cutover waits on the data pipeline,
and the pipeline was planned to wait on the platform: that loop is the
sample's `cycles` finding.
