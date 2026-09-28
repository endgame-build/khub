---
type: component
title: Tracking API
---
Serves parcel events to the storefront and the public status page. Carrier
webhooks write the events; the API only reads them. It keeps the last two
hundred events per parcel, answers from a read replica, and falls back to the
carrier's own endpoint when a parcel is older than ninety days. Responses are
cached at the edge for thirty seconds.
