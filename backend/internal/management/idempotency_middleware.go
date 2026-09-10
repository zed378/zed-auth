package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// The `Idempotency-Key` middleware.
//
// It wraps a handler and does three things around it: claim the key, run the
// handler while capturing what it answered, and store that answer — or release
// the claim, so a failure stays retryable.
//
// The header is OPTIONAL (docs/PLAN/05 says POST endpoints "support" it), so a
// request without one runs exactly as it would have. What must not happen is
// the reverse: a request WITH a key that quietly runs twice because some part
// of this was skipped.

// maxIdempotentBody bounds the request this middleware will buffer.
//
// The body has to be read in full before the handler sees it, because hashing
// it is the whole comparison. A bound is therefore not optional: without one, a
// caller sending an endless body with an Idempotency-Key would be asking this
// process to buffer it.
const maxIdempotentBody = 1 << 20 // 1 MiB

// maxStoredResponse bounds what is kept for replay.
//
// A response over this is answered normally and NOT stored — the claim is
// released instead. Storing a truncated body would replay invalid JSON to the
// retry, which is worse than making the retry run.
const maxStoredResponse = 256 << 10 // 256 KiB

// Claims is the record-keeping this middleware needs, with the transactions
// already handled.
//
// A seam rather than the store directly, and for the reason P1-09 found: every
// interesting path here — a conflict, an in-flight duplicate, a released claim,
// a handler that panicked — is reachable without a database, and a middleware
// whose branches can only be exercised against Postgres is a middleware whose
// branches mostly are not.
type Claims interface {
	Begin(ctx context.Context, orgID, clientID, key, method, path string, body []byte, now time.Time) (*Replay, error)
	Complete(ctx context.Context, orgID, clientID, key string, status int, body []byte) error
	Release(orgID, clientID, key string) error
}

// Idempotency wraps mutating handlers with replay protection.
type Idempotency struct {
	Claims Claims
	Log    *slog.Logger

	Now func() time.Time
}

func (i *Idempotency) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

// Wrap returns next, guarded by whatever Idempotency-Key the request carries.
//
// It must be registered INSIDE Require: the record is keyed by the caller's
// organization and client, and neither exists before authentication. Wrapping
// the other way round would let an unauthenticated request write rows.
func (i *Idempotency) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			// No key, no promise. docs/PLAN/05 makes the header opt-in, and a
			// caller who did not send one has not asked for anything.
			next.ServeHTTP(w, r)
			return
		}

		// Only where a replay is actually dangerous. GET and HEAD are already
		// idempotent by method, and honouring a key on them would mean caching
		// a permission-dependent read under a caller-chosen name.
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		caller, ok := CallerFrom(r.Context())
		if !ok {
			// Registered outside Require. A programming error, and answered as
			// one rather than by running the handler unprotected.
			i.log().Error("the idempotency middleware ran outside Require",
				"method", r.Method, "path", r.URL.Path)
			WriteError(w, Fault{Class: Internal, Message: "An unexpected error occurred."})
			return
		}
		if caller.ClientID == "" {
			// The record is keyed by client, so that one client cannot read
			// another's stored response by guessing a key. Without a client
			// there is no such key, and inventing one would collapse every
			// caller in an organization into a shared namespace.
			WriteError(w, Fault{
				Class:   Invalid,
				Message: "Idempotency-Key requires an access token issued to a client.",
				Reason:  "no client_id claim",
			})
			return
		}

		body, err := readBounded(w, r)
		if err != nil {
			WriteError(w, err)
			return
		}

		replay, err := i.begin(r.Context(), caller, key, r.Method, r.URL.Path, body)
		if err != nil {
			i.logFault(r, caller, key, err)
			WriteError(w, err)
			return
		}
		if replay != nil {
			writeReplay(w, replay)
			return
		}

		// From here the key is claimed, and every path out of this function
		// must either store an answer or release it. A claim left behind by a
		// panic would refuse the caller's retries for a day.
		stored := false
		defer func() {
			if !stored {
				i.release(caller, key)
			}
		}()

		r.Body = io.NopCloser(bytes.NewReader(body))
		recorder := &recordingWriter{ResponseWriter: w, limit: maxStoredResponse}
		next.ServeHTTP(recorder, r)

		if !worthStoring(recorder) {
			// Not stored, and the claim is released by the defer. A 500 must
			// stay retryable, and a 400 will be retried with a CORRECTED body —
			// which a held claim would then refuse as a conflict, leaving the
			// caller unable to fix their own mistake for twenty-four hours.
			return
		}

		if err := i.complete(r.Context(), caller, key, recorder.status, recorder.body.Bytes()); err != nil {
			// The caller already has their answer; the write of the record is
			// what failed. Logged, and the claim released so a retry runs
			// rather than replaying a record that was never written.
			i.log().Error("storing an idempotent response failed",
				"error", err.Error(), "client_id", caller.ClientID)
			return
		}
		stored = true
	})
}

func (i *Idempotency) begin(
	ctx context.Context, caller Caller, key, method, path string, body []byte,
) (*Replay, error) {
	replay, err := i.Claims.Begin(ctx, caller.OrgID, caller.ClientID, key, method, path, body, i.now())
	if err != nil {
		var fault Fault
		if errors.As(err, &fault) {
			return nil, fault
		}
		i.log().Error("claiming an idempotency key failed", "error", err.Error())
		return nil, Fault{Class: Internal, Message: "An unexpected error occurred."}
	}
	return replay, nil
}

func (i *Idempotency) complete(ctx context.Context, caller Caller, key string, status int, body []byte) error {
	return i.Claims.Complete(ctx, caller.OrgID, caller.ClientID, key, status, body)
}

// release drops an unfinished claim.
func (i *Idempotency) release(caller Caller, key string) {
	if err := i.Claims.Release(caller.OrgID, caller.ClientID, key); err != nil {
		// Not fatal to the request, which has already been answered — but it
		// does mean this key is unusable until it expires, so it is an error
		// rather than a debug line.
		i.log().Error("releasing an unfinished idempotency claim failed",
			"error", err.Error(), "client_id", caller.ClientID)
	}
}

func (i *Idempotency) logFault(r *http.Request, caller Caller, key string, err error) {
	var fault Fault
	if !errors.As(err, &fault) || fault.Reason == "" {
		return
	}
	// The key is a caller-chosen string that reaches a log line, which is why
	// ValidateKey refuses anything but printable ASCII. It is logged because a
	// conflict is nearly always a client bug and unidentifiable without it.
	i.log().Info("an idempotent request was refused",
		"reason", fault.Reason, "client_id", caller.ClientID,
		"key", key, "method", r.Method, "path", r.URL.Path)
}

func (i *Idempotency) log() *slog.Logger {
	if i.Log != nil {
		return i.Log
	}
	return slog.Default()
}

// worthStoring decides whether an answer should be replayable.
//
// Only a success. An error is a state the caller is expected to correct, and
// pinning it to their key for a day would make the correction impossible.
func worthStoring(rec *recordingWriter) bool {
	switch {
	case rec.status < 200 || rec.status > 299:
		return false
	case rec.overflowed:
		return false
	case rec.body.Len() > 0 && !json.Valid(rec.body.Bytes()):
		// Every /v1 response is JSON (docs/PLAN/05), and a replay is served with
		// `Content-Type: application/json`. A non-JSON success is therefore a
		// bug in a handler; it is answered normally, and not pinned to the
		// caller's key where it would be replayed under a content type it does
		// not match. An EMPTY body is not this case — a 204 is a real answer.
		return false
	}
	return true
}

// readBounded reads the whole request body so it can be hashed.
func readBounded(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIdempotentBody)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var overflow *http.MaxBytesError
		if errors.As(err, &overflow) {
			return nil, Fault{
				Class: Invalid, Message: "The request body is too large.",
				Reason: "body over the idempotency buffer bound",
			}
		}
		return nil, Fault{
			Class: Invalid, Message: "The request body could not be read.",
			Reason: "reading the body: " + err.Error(),
		}
	}
	return body, nil
}

// writeReplay returns a stored answer.
func writeReplay(w http.ResponseWriter, replay *Replay) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	// So a caller can tell a replay from a fresh execution. Without it, a
	// client debugging a duplicate has no way to know which of two identical
	// responses actually ran.
	h.Set("Idempotency-Replayed", "true")

	w.WriteHeader(replay.Status)
	_, _ = w.Write(replay.Response)
}

// recordingWriter captures what a handler answered while it answers.
type recordingWriter struct {
	http.ResponseWriter

	status     int
	body       bytes.Buffer
	limit      int
	overflowed bool
	wroteOnce  bool
}

func (rw *recordingWriter) WriteHeader(status int) {
	if rw.wroteOnce {
		return
	}
	rw.wroteOnce = true
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recordingWriter) Write(p []byte) (int, error) {
	if !rw.wroteOnce {
		// net/http's own implicit 200. Recorded here too, or a handler that
		// writes a body without a status would be stored as status 0.
		rw.WriteHeader(http.StatusOK)
	}

	// The caller's copy always goes out in full. Only the RECORDING is bounded:
	// truncating the response itself to fit a replay budget would corrupt the
	// answer to the request that actually ran.
	if rw.body.Len()+len(p) > rw.limit {
		rw.overflowed = true
		rw.body.Reset()
	} else if !rw.overflowed {
		rw.body.Write(p)
	}

	return rw.ResponseWriter.Write(p)
}
