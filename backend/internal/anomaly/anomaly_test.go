package anomaly

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Detection with synthetic data (P3-08).
//
// The DoD asks for impossible travel "tested with synthetic data", and the rest
// is tested the same way, because every rule here is a threshold that is easy to
// set so it fires constantly. The cases are chosen from the realistic false
// positives rather than from the attacks, since the attacks are the easy half.

// Real user agents, abbreviated to the tokens that decide.
const (
	chromeWin120  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.109 Safari/537.36"
	chromeWin121  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.6167.85 Safari/537.36"
	edgeWin       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91"
	firefoxMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0"
	safariIPhone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1"
	chromeAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.144 Mobile Safari/537.36"
)

// --- the device ---------------------------------------------------------------------------

func TestUserAgentsReduceToTheirFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, ua        string
		browser, system string
	}{
		{"chrome on windows", chromeWin120, "Chrome", "Windows"},
		{"edge claims to be chrome", edgeWin, "Edge", "Windows"},
		{"firefox on a mac", firefoxMac, "Firefox", "macOS"},
		{"safari on an iphone", safariIPhone, "Safari", "iOS"},
		{"chrome on android, which claims to be linux", chromeAndroid, "Chrome", "Android"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ParseDevice(tc.ua)
			if d.Browser != tc.browser || d.OS != tc.system {
				t.Errorf("ParseDevice = %s/%s, want %s/%s", d.Browser, d.OS, tc.browser, tc.system)
			}
		})
	}
}

// A browser update is not a new device.
//
// The single most important rule in this file for the noise budget: browsers
// update every few weeks, so a version-sensitive fingerprint would notify every
// user on every update and the notices would be ignored within a month.
func TestABrowserUpdateIsTheSameDevice(t *testing.T) {
	if ParseDevice(chromeWin120) != ParseDevice(chromeWin121) {
		t.Errorf("Chrome 120 and 121 on the same OS are different devices: %+v vs %+v",
			ParseDevice(chromeWin120), ParseDevice(chromeWin121))
	}
}

// An empty user agent is unknown and never compared.
func TestAnEmptyUserAgentIsUnknown(t *testing.T) {
	for _, ua := range []string{"", "   ", "curl/8.4.0", "Go-http-client/1.1"} {
		if ParseDevice(ua).Known() {
			t.Errorf("ParseDevice(%q) is Known; every API login would notify", ua)
		}
	}
}

// An absurdly long user agent is bounded before a regex sees it.
func TestAHugeUserAgentIsBounded(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) + " Chrome/120 Windows"
	start := time.Now()
	ParseDevice(huge)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("parsing a 1 MiB user agent took %s", elapsed)
	}
}

// --- travel, with synthetic coordinates ---------------------------------------------------

var (
	jakarta   = Location{City: "Jakarta", Country: "ID", Latitude: -6.2088, Longitude: 106.8456}
	bandung   = Location{City: "Bandung", Country: "ID", Latitude: -6.9175, Longitude: 107.6191}
	singapore = Location{City: "Singapore", Country: "SG", Latitude: 1.3521, Longitude: 103.8198}
	london    = Location{City: "London", Country: "GB", Latitude: 51.5074, Longitude: -0.1278}
	nowhere   = Location{}
)

// The distance computation is right, checked against an independent computation
// rather than against itself.
//
// The expected figures come from the spherical law of cosines, evaluated
// separately from the haversine under test — two different formulas agreeing on
// the same coordinates. An earlier version used a remembered "~880 km" for
// Jakarta–Singapore and failed: that is roughly the airport-to-airport distance,
// and these are city-centre coordinates, 905 km apart. The code was right and
// the number from memory was not.
func TestTheDistanceMatchesKnownRoutes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		a, b   Location
		wantKM float64
	}{
		{"Jakarta to Singapore", jakarta, singapore, 905},
		{"Jakarta to London", jakarta, london, 11718},
		{"a point to itself", jakarta, jakarta, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DistanceKM(tc.a, tc.b)
			// 2% tolerance: the mean earth radius is within 0.5%, and the
			// published figures are rounded.
			if math.Abs(got-tc.wantKM) > math.Max(tc.wantKM*0.02, 1) {
				t.Errorf("DistanceKM = %.0f km, want ~%.0f km", got, tc.wantKM)
			}
		})
	}
}

func TestImpossibleTravelDecidesEveryCase(t *testing.T) {
	for _, tc := range []struct {
		name    string
		a, b    Location
		elapsed time.Duration
		want    bool
		why     string
	}{
		{"Jakarta then London twenty minutes later", jakarta, london, 20 * time.Minute, true,
			"11,700 km in twenty minutes is the credential being used somewhere else"},
		{"Jakarta then London fifteen hours later", jakarta, london, 15 * time.Hour, false,
			"a real flight — ~780 km/h, below a jet's cruise"},
		{"Jakarta then Singapore two hours later", jakarta, singapore, 2 * time.Hour, false,
			"a short flight is a trip people take"},
		{"Jakarta then Bandung ten minutes later", jakarta, bandung, 10 * time.Minute, false,
			"120 km is under the minimum — mobile geolocation is wrong by that much routinely"},
		{"one location unknown", jakarta, nowhere, time.Minute, false,
			"absence of data is not evidence of travel"},
		{"both at the same instant, far apart", jakarta, london, 0, true,
			"zero elapsed time makes any real distance impossible, rather than a divide by zero"},
		{"out of order", jakarta, london, -time.Hour, true,
			"clock skew between logins is treated as zero elapsed, not as a negative speed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImpossibleTravel(tc.a, tc.b, tc.elapsed); got != tc.want {
				t.Errorf("ImpossibleTravel = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// Uncertainty is subtracted, so two coarse fixes cannot manufacture a trip.
func TestLocationUncertaintyIsSubtracted(t *testing.T) {
	// Jakarta and Singapore are ~905 km apart. With each fix only good to
	// 400 km, they are not demonstrably more than ~105 km apart — under the
	// minimum, so not travel.
	vagueA := jakarta
	vagueA.AccuracyKM = 400
	vagueB := singapore
	vagueB.AccuracyKM = 400

	if ImpossibleTravel(vagueA, vagueB, time.Minute) {
		t.Error("two uncertain fixes were treated as a precise jump")
	}
	if !ImpossibleTravel(jakarta, singapore, time.Minute) {
		t.Error("the same trip with precise fixes was not flagged; the test above proves nothing")
	}
}

// --- the assessment ---------------------------------------------------------------------------

func login(at time.Time, ua string, loc Location) Login {
	return Login{At: at, Device: ParseDevice(ua), Location: loc}
}

// A user's first login is never an anomaly.
func TestAFirstLoginIsNeverAnAnomaly(t *testing.T) {
	now := time.Now()
	if got := Assess(login(now, chromeWin120, london), nil); len(got) != 0 {
		t.Errorf("a first login produced %v; every new user would get a security warning on day one", got)
	}
}

// A login matching history produces nothing.
func TestAFamiliarLoginIsQuiet(t *testing.T) {
	now := time.Now()
	history := []Login{login(now.Add(-24*time.Hour), chromeWin120, jakarta)}

	if got := Assess(login(now, chromeWin121, jakarta), history); len(got) != 0 {
		t.Errorf("a routine login after a browser update produced %v", got)
	}
}

func TestANewDeviceIsDetected(t *testing.T) {
	now := time.Now()
	history := []Login{login(now.Add(-24*time.Hour), chromeWin120, jakarta)}

	got := Assess(login(now, safariIPhone, jakarta), history)
	if !has(got, SignalNewDevice) {
		t.Errorf("a new device produced %v", got)
	}
	if has(got, SignalNewLocation) {
		t.Errorf("the same city was reported as a new location: %v", got)
	}
}

func TestANewLocationIsDetected(t *testing.T) {
	now := time.Now()
	history := []Login{login(now.Add(-48*time.Hour), chromeWin120, jakarta)}

	got := Assess(login(now, chromeWin120, singapore), history)
	if !has(got, SignalNewLocation) {
		t.Errorf("a new city produced %v", got)
	}
	if has(got, SignalImpossibleTravel) {
		t.Errorf("a trip two days apart was reported as impossible: %v", got)
	}
}

// Travel is judged against the most recent login only.
//
// Against every past login, a user who flew home a month ago would look as
// though they had just made an impossible trip from wherever they were before.
func TestTravelIsJudgedAgainstTheMostRecentLogin(t *testing.T) {
	now := time.Now()
	history := []Login{
		login(now.Add(-time.Hour), chromeWin120, jakarta),      // most recent
		login(now.Add(-30*24*time.Hour), chromeWin120, london), // a month ago
	}

	got := Assess(login(now, chromeWin120, jakarta), history)
	if has(got, SignalImpossibleTravel) {
		t.Errorf("a login from the same city as an hour ago was flagged as travel: %v", got)
	}
}

func TestImpossibleTravelIsDetectedInAssessment(t *testing.T) {
	now := time.Now()
	history := []Login{login(now.Add(-20*time.Minute), chromeWin120, jakarta)}

	got := Assess(login(now, chromeWin120, london), history)
	if !has(got, SignalImpossibleTravel) {
		t.Errorf("Jakarta then London twenty minutes later produced %v", got)
	}
}

// An unknown location is never a new one.
func TestAnUnknownLocationIsNotNew(t *testing.T) {
	now := time.Now()
	history := []Login{login(now.Add(-time.Hour), chromeWin120, jakarta)}

	if got := Assess(login(now, chromeWin120, nowhere), history); has(got, SignalNewLocation) {
		t.Errorf("an unresolvable IP was reported as a new location: %v", got)
	}
}

func has(signals []Signal, want Signal) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}
