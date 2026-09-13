package login

import (
	"context"
	"time"

	"github.com/zed378/zed-auth/backend/internal/anomaly"
	"github.com/zed378/zed-auth/backend/internal/session"
)

// Running login anomaly detection (P3-08).
//
// **Detection must not fail or delay a login** (spec § 4). anomaly.Detector
// already swallows every error; this makes it off the request path as well,
// because a history read and — with notifications on — an SMTP round trip are
// latency a person waiting for a sign-in should never pay for a check that
// cannot change its outcome.

// AnomalyDetector is anomaly.Detector's one method.
type AnomalyDetector interface {
	Observe(ctx context.Context, c anomaly.Current)
}

// anomalyTimeout bounds one detection run.
//
// Detached from the request, so without a bound of its own a hung SMTP server
// would accumulate goroutines at the rate people sign in.
const anomalyTimeout = 30 * time.Second

// detectAnomalies starts detection for a session just committed.
//
// The context is detached from the request's cancellation — the response is
// about to be written, and cancelling detection because the browser followed
// its redirect would mean it never ran — but keeps its values, so a trace
// still joins the login that caused it.
func (h *Handler) detectAnomalies(ctx context.Context, s session.Session, ip, userAgent string, at time.Time) {
	if h.Anomalies == nil || s.ID == "" {
		return
	}

	current := anomaly.Current{
		OrgID:     s.OrgID,
		UserID:    s.UserID,
		SessionID: s.ID,
		At:        at,
		IP:        ip,
		UserAgent: userAgent,
	}
	detached := context.WithoutCancel(ctx)

	go func() {
		// A panic here would take the whole process down with it, which is
		// every other user's login too. Recovered and logged.
		defer func() {
			if rec := recover(); rec != nil && h.Log != nil {
				h.Log.Error("login anomaly detection panicked", "panic", rec)
			}
		}()

		runCtx, cancel := context.WithTimeout(detached, anomalyTimeout)
		defer cancel()
		h.Anomalies.Observe(runCtx, current)
	}()
}
