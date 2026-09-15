package authorize

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The MFA mandate on the silent path (P3-14, P3-07's A-2).
//
// A session opened before the mandate's grace ended used to keep issuing codes
// with no interaction for its whole lifetime. The mandate applied only to
// people who typed a password.

type fakeMandate struct {
	unmet bool
	err   error
	asked []string
}

func (m *fakeMandate) Unmet(_ context.Context, orgID, userID string, _ time.Time) (bool, error) {
	m.asked = append(m.asked, orgID+"/"+userID)
	return m.unmet, m.err
}

func silentWith(t *testing.T, mandate *fakeMandate) string {
	t.Helper()
	h := handler(t, fakeClients{app: webApp()}, fakeSessions{s: liveSession()}, newStoreForTest(t))
	h.Mandate = mandate
	return do(h, validQuery(), strings.Repeat("A", 43)).Header().Get("Location")
}

func TestAnUnmetMandateSendsALiveSessionToSignIn(t *testing.T) {
	mandate := &fakeMandate{unmet: true}
	if location := silentWith(t, mandate); !strings.HasPrefix(location, "/login?") {
		t.Errorf("a session whose user must enrol was used silently: %q", location)
	}
	if len(mandate.asked) != 1 || mandate.asked[0] != testOrgID+"/"+liveSession().UserID {
		t.Errorf("the mandate was asked about %v, want the session's own organization and user", mandate.asked)
	}
}

func TestAMandateThatCannotBeReadSendsToSignIn(t *testing.T) {
	if location := silentWith(t, &fakeMandate{err: errors.New("database gone")}); !strings.HasPrefix(location, "/login?") {
		t.Errorf("an unreadable mandate issued a code silently: %q", location)
	}
}

// Positive control: a met mandate leaves silent SSO exactly as it was.
func TestAMetMandateLeavesSilentSSOAlone(t *testing.T) {
	if location := silentWith(t, &fakeMandate{}); !strings.HasPrefix(location, testRedirect) {
		t.Errorf("a satisfied mandate stopped silent SSO: %q", location)
	}
}
