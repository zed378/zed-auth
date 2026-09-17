# Category: WEBHOOK

Asynchronous event notification delivery system, event payload schemas, HMAC-SHA256 signature verification, and delivery retry queues.

## Category Mandate

The `WEBHOOK/` directory specifies **outbound event notifications**. It details event categories, payload schemas, HMAC signature headers, and exponential backoff retry mechanisms.

## Documents in Category

| Document | Title | Description |
|---|---|---|
| `00-WEBHOOK-OVERVIEW.md` | Webhook Overview | Architecture of outbound webhook engine. |
| `01-EVENT-TYPES-AND-PAYLOADS.md` | Event Types & Payloads | Catalog of event types (`user.created`, `role.assigned`). |
| `02-HMAC-SIGNATURE-VERIFICATION.md` | HMAC Signature Verification | Header `X-Auth-Signature` calculation with SHA-256. |
| `03-DELIVERY-RETRY-AND-DEAD-LETTER.md` | Delivery Retry & Dead Letter | Exponential backoff retry policy & dead-letter queue. |
