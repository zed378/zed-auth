package auditlog

import "encoding/json"

// decodePayload turns the stored jsonb into a map.
//
// A failure yields nil rather than an error: the payload is detail, and one
// unreadable row must not take down a page of the audit log an investigator is
// reading. The rest of the entry — who, what, when — is intact and is the part
// that carries the timeline.
func decodePayload(raw string) map[string]any {
	if raw == "" || raw == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
