package anomaly

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Running detection after a login (P3-08).
//
// **Nothing here can fail a login.** Observe returns nothing and swallows every
// error: it runs after the session has been committed, and a detector that could
// turn a successful sign-in into a 500 would be a denial of service against every
// user whenever the geolocation file was unreadable or the audit table was slow.
//
// And **notification is separate from detection**, on purpose. Detection and its
// audit row and metric always run; notifying the user needs a Notifier, which a
// deployment supplies only when it chooses to. The card requires the
// false-positive rate to be measured against real traffic before notifications
// go out broadly — this is what makes that measurement possible without anybody
// having been emailed yet.

// Past is one earlier successful login, as the history store returns it.
type Past struct {
	At        time.Time
	IP        string
	UserAgent string
}

// History reads a user's recent logins.
type History interface {
	// Recent returns earlier logins, most recent first, excluding the session
	// just created — otherwise every login would match itself and nothing
	// would ever be new.
	Recent(ctx context.Context, orgID, userID, excludeSessionID string, limit int) ([]Past, error)
}

// Finding is what a detection produced, for the audit row and the notice.
type Finding struct {
	OrgID     string
	UserID    string
	SessionID string
	At        time.Time
	Signals   []Signal

	// Where is the coarse location, "City, CC", or empty when unknown.
	Where string
}

// Recorder writes the audit row. Always called when there is a finding.
type Recorder interface {
	RecordAnomaly(ctx context.Context, f Finding) error
}

// Notifier tells the user. Nil on a Detector means notifications are off.
type Notifier interface {
	NotifyAnomaly(ctx context.Context, f Finding) error
}

// Observer counts findings for monitoring.
type Observer interface {
	Anomaly(signal Signal)
}

// Detector runs the whole thing.
type Detector struct {
	History  History
	Locator  Locator
	Recorder Recorder

	// Notifier is nil unless a deployment has turned notifications on.
	Notifier Notifier

	Observer Observer
	Log      *slog.Logger
}

// historyDepth is how many past logins are compared against.
//
// Twenty: enough to cover a user's handful of devices and usual places over the
// last weeks, and bounded so the read stays cheap on a path every login takes.
// A device used once, twenty logins ago, is reasonably treated as new again.
const historyDepth = 20

// Current is the login just completed.
type Current struct {
	OrgID     string
	UserID    string
	SessionID string
	At        time.Time
	IP        string
	UserAgent string
}

// Observe assesses one login. It never fails and never blocks the caller on an
// error it could not recover from.
func (d *Detector) Observe(ctx context.Context, c Current) {
	if d == nil || d.History == nil {
		return
	}

	past, err := d.History.Recent(ctx, c.OrgID, c.UserID, c.SessionID, historyDepth)
	if err != nil {
		d.warn("reading login history for anomaly detection failed", err)
		return
	}

	locator := d.Locator
	if locator == nil {
		locator = Unknown{}
	}

	current := Login{At: c.At, Device: ParseDevice(c.UserAgent), Location: locator.Locate(c.IP)}

	history := make([]Login, 0, len(past))
	for _, p := range past {
		history = append(history, Login{
			At:       p.At,
			Device:   ParseDevice(p.UserAgent),
			Location: locator.Locate(p.IP),
		})
	}

	signals := Assess(current, history)
	if len(signals) == 0 {
		return
	}

	finding := Finding{
		OrgID:     c.OrgID,
		UserID:    c.UserID,
		SessionID: c.SessionID,
		At:        c.At,
		Signals:   signals,
		Where:     current.Location.Describe(),
	}

	if d.Observer != nil {
		for _, s := range signals {
			d.Observer.Anomaly(s)
		}
	}

	if d.Recorder != nil {
		if err := d.Recorder.RecordAnomaly(ctx, finding); err != nil {
			d.warn("recording a login anomaly failed", err)
		}
	}

	if d.Notifier != nil {
		if err := d.Notifier.NotifyAnomaly(ctx, finding); err != nil {
			d.warn("notifying a user of a login anomaly failed", err)
		}
	}
}

func (d *Detector) warn(msg string, err error) {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	log.Warn(msg, "error", err.Error())
}

// Describe renders a coarse location for a person to read: "City, CC", or the
// country alone, or empty when unknown.
//
// Distinct from Place, which is a comparison key and not something to show
// anybody. The sessions API (P3-09) uses this one.
func (l Location) Describe() string {
	if !l.Known() {
		return ""
	}
	if l.City == "" {
		return l.Country
	}
	return l.City + ", " + l.Country
}

// Reasons turns signals into sentences for the notice.
//
// Plain language a person recognises, and never an accusation — see
// mail.LoginAnomaly for why the notice avoids "your account may be
// compromised".
func Reasons(signals []Signal) []string {
	out := make([]string, 0, len(signals))
	for _, s := range signals {
		switch s {
		case SignalNewDevice:
			out = append(out, "It came from a browser or device you have not used to sign in before.")
		case SignalNewLocation:
			out = append(out, "It came from a location you have not signed in from before.")
		case SignalImpossibleTravel:
			out = append(out, "It came from somewhere too far from your previous sign-in to have "+
				"travelled between them in the time since.")
		}
	}
	return out
}

// SignalNames renders signals for an audit payload.
func SignalNames(signals []Signal) []string {
	out := make([]string, 0, len(signals))
	for _, s := range signals {
		out = append(out, strings.TrimSpace(string(s)))
	}
	return out
}
