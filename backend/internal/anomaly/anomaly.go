// Package anomaly notices logins that do not look like the user (P3-08).
//
// Specification: MEMORY/specs/P3-08-login-anomaly-detection.md.
//
// The constraint that shapes all of it is the card's step 5: **a notification on
// every login trains people to ignore them, which is worse than none.** So every
// rule below is written to be quiet by default — a browser update is not a new
// device, an unknown location is not a new one, and a user's first login is never
// an anomaly, because there is nothing for it to be unlike.
//
// Nothing in this package can refuse a login. It runs after the session exists,
// and the decision to notify rather than to step up is ADR-024.
package anomaly

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// Signal is one reason a login looks unusual.
type Signal string

const (
	SignalNewDevice        Signal = "new_device"
	SignalNewLocation      Signal = "new_location"
	SignalImpossibleTravel Signal = "impossible_travel"
)

// --- the device ----------------------------------------------------------------------

// Device is a user agent reduced to what is stable across a device's life.
//
// **Browser family and OS family, and nothing finer.** The card forbids invasive
// fingerprinting, and it would be counter-productive anyway: browsers update
// every few weeks, so comparing full version strings would notify every user on
// every update and the notices would be ignored within a month.
type Device struct {
	Browser string
	OS      string
}

// Known reports whether anything was recognised.
//
// An unknown device is never compared. An empty user agent — every API client,
// every script — would otherwise match nothing and notify on every login.
func (d Device) Known() bool { return d.Browser != "" && d.OS != "" }

// browsers are checked in order, and the order matters: Edge and Opera both
// claim to be Chrome, and Chrome claims to be Safari. The more specific token is
// matched first so the family is the one the user actually runs.
var browsers = []struct {
	family string
	token  *regexp.Regexp
}{
	{"Edge", regexp.MustCompile(`\bEdg(e|A|iOS)?/`)},
	{"Opera", regexp.MustCompile(`\b(OPR|Opera)/`)},
	{"Firefox", regexp.MustCompile(`\b(Firefox|FxiOS)/`)},
	{"Chrome", regexp.MustCompile(`\b(Chrome|CriOS)/`)},
	{"Safari", regexp.MustCompile(`\bVersion/[\d.]+.*\bSafari/`)},
}

var systems = []struct {
	family string
	token  *regexp.Regexp
}{
	// iOS before macOS: an iPad presents a Macintosh user agent and says
	// "Mobile" only in some modes, so the phone and tablet tokens are matched
	// first.
	{"iOS", regexp.MustCompile(`\b(iPhone|iPad|iPod)\b`)},
	{"Android", regexp.MustCompile(`\bAndroid\b`)},
	{"Windows", regexp.MustCompile(`\bWindows\b`)},
	{"macOS", regexp.MustCompile(`\bMac OS X\b|\bMacintosh\b`)},
	{"ChromeOS", regexp.MustCompile(`\bCrOS\b`)},
	{"Linux", regexp.MustCompile(`\bLinux\b`)},
}

// maxUserAgent bounds what is matched. A user agent is attacker-supplied, and a
// regular expression over an unbounded string is a place to spend CPU.
const maxUserAgent = 512

// ParseDevice reduces a user agent to its families.
func ParseDevice(userAgent string) Device {
	ua := strings.TrimSpace(userAgent)
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	if ua == "" {
		return Device{}
	}

	var d Device
	for _, b := range browsers {
		if b.token.MatchString(ua) {
			d.Browser = b.family
			break
		}
	}
	for _, s := range systems {
		if s.token.MatchString(ua) {
			d.OS = s.family
			break
		}
	}
	return d
}

// --- the location ----------------------------------------------------------------------

// Location is where an IP resolves to, coarsely.
//
// **City-level, and never stored.** It is derived at detection time from the IP
// the session already holds. Persisting it would put a movement history of every
// user into a long-retention table — which is the privacy posture the card warns
// an identity provider against — and it would go stale whenever the geolocation
// data was updated.
type Location struct {
	City      string
	Country   string
	Latitude  float64
	Longitude float64

	// Accuracy is the radius, in kilometres, the source claims for the point.
	// Travel detection subtracts it: two points 200 km apart, each accurate to
	// 100 km, may be the same place.
	AccuracyKM float64
}

// Known reports whether the source resolved anything useful.
func (l Location) Known() bool { return l.Country != "" }

// Place is what two logins are compared by for "new location".
//
// City and country together. Country alone would miss a user in one country
// signing in from across it; coordinates alone would treat every data update's
// small shift as a move.
func (l Location) Place() string {
	if !l.Known() {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(l.City)) + "|" + strings.ToLower(l.Country)
}

// --- travel ----------------------------------------------------------------------------------

// MaxPlausibleSpeedKMH is the fastest a person could travel between two logins.
//
// **1,000 km/h — a commercial jet at cruise.** Faster than that and no human
// made both logins. It deliberately does not add airport time: a threshold that
// assumed an hour each side would miss the realistic attack (a credential used
// from another continent twenty minutes after the owner's own login) in order to
// avoid flagging a trip nobody makes.
const MaxPlausibleSpeedKMH = 1000.0

// MinTravelDistanceKM is the distance below which nothing is travel.
//
// Geolocation of mobile and residential IPs is often wrong by tens of
// kilometres, and a phone moving between towers or a laptop between an office
// and a café would otherwise look like a jump.
const MinTravelDistanceKM = 300.0

// earthRadiusKM is the mean radius, which is accurate to within 0.5% for this.
const earthRadiusKM = 6371.0

// DistanceKM is the great-circle distance between two points (haversine).
func DistanceKM(a, b Location) float64 {
	lat1, lat2 := radians(a.Latitude), radians(b.Latitude)
	dLat := radians(b.Latitude - a.Latitude)
	dLon := radians(b.Longitude - a.Longitude)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Min(1, math.Sqrt(h)))
}

func radians(deg float64) float64 { return deg * math.Pi / 180 }

// ImpossibleTravel reports whether two logins could not both have been made by
// one person.
//
// **Absent data is never travel.** Either location unknown, and the answer is no
// — an absence of evidence is not evidence of movement, and guessing would
// notify every user whose provider the database does not know.
func ImpossibleTravel(earlier, later Location, elapsed time.Duration) bool {
	if !earlier.Known() || !later.Known() {
		return false
	}

	// Subtract the uncertainty of both points. Two fixes 400 km apart, each
	// only good to 150 km, are not demonstrably more than 100 km apart.
	distance := DistanceKM(earlier, later) - earlier.AccuracyKM - later.AccuracyKM
	if distance < MinTravelDistanceKM {
		return false
	}

	// Logins in the same instant, or out of order, are treated as zero elapsed
	// time — which makes any real distance impossible rather than dividing by
	// zero.
	hours := elapsed.Hours()
	if hours <= 0 {
		return true
	}
	return distance/hours > MaxPlausibleSpeedKMH
}

// --- the decision ------------------------------------------------------------------------------

// Login is one sign-in, as detection sees it.
type Login struct {
	At       time.Time
	Device   Device
	Location Location
}

// Assess compares a login against the user's history.
//
// `history` is prior successful logins, most recent first. **An empty history is
// never an anomaly**: a user's first sign-in has nothing to be unlike, and
// notifying on it would send every new user a security warning on their first
// day — which is the fastest way to teach them to ignore the next one.
func Assess(current Login, history []Login) []Signal {
	if len(history) == 0 {
		return nil
	}

	var signals []Signal

	if current.Device.Known() {
		seen := false
		for _, past := range history {
			if past.Device == current.Device {
				seen = true
				break
			}
		}
		if !seen {
			signals = append(signals, SignalNewDevice)
		}
	}

	if current.Location.Known() {
		seen := false
		for _, past := range history {
			if past.Location.Known() && past.Location.Place() == current.Location.Place() {
				seen = true
				break
			}
		}
		if !seen {
			signals = append(signals, SignalNewLocation)
		}

		// Against the MOST RECENT login only. Comparing against every past
		// login would flag a user who flew home a month ago as having made an
		// impossible trip from wherever they were before that.
		previous := history[0]
		if ImpossibleTravel(previous.Location, current.Location, current.At.Sub(previous.At)) {
			signals = append(signals, SignalImpossibleTravel)
		}
	}

	return signals
}
