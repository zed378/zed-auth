package management

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
)

// Keeping the raw request body available to a handler.
//
// This exists because of a gap between the contract and the generated code.
// `additionalProperties: false` in openapi.yaml is a documentation statement:
// oapi-codegen renders it as a struct with named fields and **nothing rejects
// an unknown key at runtime** — json.Unmarshal simply discards it.
//
// For most fields that is harmless. For an organization's `settings` it is the
// exact failure the validation exists to prevent: `mfa_requried: true` would be
// silently dropped, the response would echo back a policy that does not contain
// it, and an administrator would believe MFA was on. So the handler compares
// the keys the caller actually sent against the schema, which means it needs
// the bytes rather than the decoded struct.
//
// Buffering it once here rather than in each handler also removes a
// double-read: the idempotency middleware needs the same bytes to hash.

// maxRequestBody bounds what any /v1 request may send.
//
// A management request is a handful of fields. The bound is generous against
// that and exists so that "the server buffers the body" is not an invitation.
const maxRequestBody = 1 << 20 // 1 MiB

type bodyKey struct{}

// RawBody returns the bytes of the request body, if BufferBody ran.
//
// The bool is not decoration: a handler reached without the middleware would
// otherwise see an empty body and conclude the caller sent no settings, which
// is the same wrong answer as dropping the keys.
func RawBody(ctx context.Context) ([]byte, bool) {
	b, ok := ctx.Value(bodyKey{}).([]byte)
	return b, ok
}

// BufferBody reads the body once and makes it available to everything after it.
//
// `r.Body` is replaced with a fresh reader over the same bytes, so the
// generated wrapper decodes exactly what was hashed and exactly what the
// handler inspects. Three views of one array, which is the point: a handler
// checking keys the decoder never saw would be checking a different request.
func BufferBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var overflow *http.MaxBytesError
			if errors.As(err, &overflow) {
				WriteError(w, Fault{
					Class: Invalid, Message: "The request body is too large.",
					Reason: "body over the buffer bound",
				})
				return
			}
			WriteError(w, Fault{
				Class: Invalid, Message: "The request body could not be read.",
				Reason: "reading the body: " + err.Error(),
			})
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), bodyKey{}, body)))
	})
}
