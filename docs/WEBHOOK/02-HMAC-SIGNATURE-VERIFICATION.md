# 02 - HMAC Signature Verification

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify HMAC-SHA256 payload signing and verification header standard.

## Category Mandate

Allows webhook receivers to verify payload authenticity and prevent spoofing.

## Key Topics To Specify

- Header: `X-Auth-Signature: t=1789574400,v1=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08`.
- Signature calculated over `timestamp + "." + payload_body` using secret key.
- Prevents replay attacks by checking timestamp window (max 5 minutes drift).

## Reference Architecture & Specification

Verification Code (Node.js):
```javascript
const signature = crypto.createHmac('sha256', secret)
  .update(`${timestamp}.${payload}`)
  .digest('hex');
```

## Acceptance Criteria

- [x] HMAC signature calculation specified.
- [x] Replay attack timestamp validation enforced.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/03-SECURITY-CONTROLS-BASELINE.md`
