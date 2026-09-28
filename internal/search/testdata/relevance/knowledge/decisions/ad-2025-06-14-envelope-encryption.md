---
type: adr
title: Adopt envelope encryption for PII
---
Personal data is encrypted with per-record data keys wrapped by a master key,
so rotating the master key never rewrites the rows.
