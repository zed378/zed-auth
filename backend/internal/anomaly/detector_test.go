package anomaly

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The detector's wiring (P3-08).
//
// The assessment rules are tested in anomaly_test.go. What is here is what the
// detector does with them — and the two properties that matter are both about
// what it must NOT do: it must never fail a login, and it must never notify
// anybody unless a deployment has chosen to.

type fakeHistory struct {
	past     []Past
	err      error
	seenUser string
	seenOrg  string
	seenExcl string
}

func (f *fakeHistory) Recent(_ context.Context, orgID, userID, exclude string, _ int) ([]Past, error) {
	f.seenOrg, f.seenUser, f.seenExcl = orgID, userID, exclude
	return f.past, f.err
}

type fakeRecorder struct {
	findings []Finding
	err      error
}

func (f *fakeRecorder) RecordAnomaly(_ context.Context, finding Finding) error {
	f.findings = append(f.findings, finding)
	return f.err
}

type fakeNotifier struct {
	findings []Finding
	err      error
}

func (f *fakeNotifier) NotifyAnomaly(_ context.Context, finding Finding) error {
	f.findings = append(f.findings, finding)
	return f.err
}

type fakeObserver struct{ counts map[Signal]int }

func (f *fakeObserver) Anomaly(s Signal) {
	if f.counts == nil {
		f.counts = map[Signal]int{}
	}
	f.counts[s]++
}

// fixedLocator resolves two documentation IPs to fixed places.
type fixedLocator map[string]Location

func (f fixedLocator) Locate(ip string) Location { return f[ip] }

func current(ua, ip string) Current {
	return Current{
		OrgID: "org-1", UserID: "user-1", SessionID: "session-new",
		At: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC), IP: ip, UserAgent: ua,
	}
}

// --- detection runs -----------------------------------------------------------------

func TestANewDeviceIsRecordedAndCounted(t *testing.T) {
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), UserAgent: chromeWin120},
	}}
	recorder := &fakeRecorder{}
	observer := &fakeObserver{}
	d := &Detector{History: history, Recorder: recorder, Observer: observer}

	d.Observe(context.Background(), current(safariIPhone, ""))

	if len(recorder.findings) != 1 {
		t.Fatalf("recorded %d findings, want 1", len(recorder.findings))
	}
	if !has(recorder.findings[0].Signals, SignalNewDevice) {
		t.Errorf("the finding is %v, missing new_device", recorder.findings[0].Signals)
	}
	if observer.counts[SignalNewDevice] != 1 {
		t.Errorf("the metric counted %d new-device findings, want 1", observer.counts[SignalNewDevice])
	}
}

// The history read excludes the session just created — otherwise every login
// would match itself and nothing would ever be new.
func TestTheCurrentSessionIsExcludedFromHistory(t *testing.T) {
	history := &fakeHistory{}
	d := &Detector{History: history}

	d.Observe(context.Background(), current(chromeWin120, ""))

	if history.seenExcl != "session-new" {
		t.Errorf("history was read excluding %q, want the new session", history.seenExcl)
	}
	if history.seenUser != "user-1" || history.seenOrg != "org-1" {
		t.Errorf("history was read for org=%q user=%q, want org-1/user-1", history.seenOrg, history.seenUser)
	}
}

// A familiar login records nothing and notifies nobody.
func TestAFamiliarLoginIsSilent(t *testing.T) {
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), UserAgent: chromeWin120},
	}}
	recorder := &fakeRecorder{}
	notifier := &fakeNotifier{}
	d := &Detector{History: history, Recorder: recorder, Notifier: notifier}

	d.Observe(context.Background(), current(chromeWin121, ""))

	if len(recorder.findings) != 0 || len(notifier.findings) != 0 {
		t.Errorf("a routine login after a browser update produced %d records and %d notices",
			len(recorder.findings), len(notifier.findings))
	}
}

// --- notification is a separate choice ----------------------------------------------------

// With no Notifier, a finding is RECORDED and nobody is emailed.
//
// This is what lets the false-positive rate be measured on real traffic before
// notifications are turned on — the card's own precondition, and the reason
// detection and notification are not one switch.
func TestWithoutANotifierFindingsAreRecordedButNotSent(t *testing.T) {
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), UserAgent: chromeWin120},
	}}
	recorder := &fakeRecorder{}
	d := &Detector{History: history, Recorder: recorder} // Notifier deliberately nil

	d.Observe(context.Background(), current(safariIPhone, ""))

	if len(recorder.findings) != 1 {
		t.Errorf("a finding with notifications off was not recorded; the rate could never be measured")
	}
}

func TestWithANotifierTheUserIsTold(t *testing.T) {
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), UserAgent: chromeWin120},
	}}
	notifier := &fakeNotifier{}
	d := &Detector{History: history, Recorder: &fakeRecorder{}, Notifier: notifier}

	d.Observe(context.Background(), current(safariIPhone, ""))

	if len(notifier.findings) != 1 {
		t.Errorf("notified %d times, want 1", len(notifier.findings))
	}
}

// --- nothing can fail a login -----------------------------------------------------------

// Every dependency failing at once does not panic and does not propagate.
//
// Observe has no error return by design; this asserts the design holds when
// everything behind it is broken, which is exactly when a detector that could
// fail a login would do the most damage.
func TestFailuresEverywhereAreSwallowed(t *testing.T) {
	boom := errors.New("down")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Observe panicked: %v", r)
		}
	}()

	// History fails.
	(&Detector{History: &fakeHistory{err: boom}}).Observe(context.Background(), current(safariIPhone, ""))

	// History succeeds; recording and notifying both fail.
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), UserAgent: chromeWin120},
	}}
	notifier := &fakeNotifier{err: boom}
	(&Detector{
		History:  history,
		Recorder: &fakeRecorder{err: boom},
		Notifier: notifier,
	}).Observe(context.Background(), current(safariIPhone, ""))

	// A failed audit write must not stop the notice: the user being told is
	// worth more than the row, and one failing is no reason to skip the other.
	if len(notifier.findings) != 1 {
		t.Error("a failed audit write suppressed the notification")
	}
}

// A nil detector, or one with no history, is a no-op rather than a panic.
func TestAnUnconfiguredDetectorDoesNothing(t *testing.T) {
	var d *Detector
	d.Observe(context.Background(), current(safariIPhone, ""))
	(&Detector{}).Observe(context.Background(), current(safariIPhone, ""))
}

// --- location flows through ----------------------------------------------------------------

func TestTheCoarseLocationReachesTheFinding(t *testing.T) {
	history := &fakeHistory{past: []Past{
		{At: time.Date(2026, 9, 13, 11, 40, 0, 0, time.UTC), UserAgent: chromeWin120, IP: "198.51.100.1"},
	}}
	recorder := &fakeRecorder{}
	d := &Detector{
		History:  history,
		Recorder: recorder,
		Locator: fixedLocator{
			"198.51.100.1": jakarta,
			"203.0.113.1":  london,
		},
	}

	// Twenty minutes after a Jakarta login, from London.
	d.Observe(context.Background(), current(chromeWin120, "203.0.113.1"))

	if len(recorder.findings) != 1 {
		t.Fatalf("recorded %d findings, want 1", len(recorder.findings))
	}
	f := recorder.findings[0]
	if !has(f.Signals, SignalImpossibleTravel) {
		t.Errorf("signals = %v, missing impossible_travel", f.Signals)
	}
	if f.Where != "London, GB" {
		t.Errorf("Where = %q, want \"London, GB\"", f.Where)
	}
}

// --- the words -----------------------------------------------------------------------------

// Every signal has a sentence, and none of them accuses the user.
func TestEverySignalHasAPlainReason(t *testing.T) {
	signals := []Signal{SignalNewDevice, SignalNewLocation, SignalImpossibleTravel}
	reasons := Reasons(signals)

	if len(reasons) != len(signals) {
		t.Fatalf("got %d reasons for %d signals — a signal would reach the notice as nothing", len(reasons), len(signals))
	}
	for _, r := range reasons {
		lower := strings.ToLower(r)
		for _, alarm := range []string{"compromised", "hacked", "attack", "stolen"} {
			if strings.Contains(lower, alarm) {
				t.Errorf("reason %q uses %q; most of these are a new laptop, and alarm teaches people to delete the notice", r, alarm)
			}
		}
	}
}
