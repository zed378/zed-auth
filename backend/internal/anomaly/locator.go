package anomaly

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Where an IP is, coarsely (P3-08).
//
// **No plan document names a geolocation source**, and both new-location and
// impossible-travel detection need one — recorded as `PG-41`. The realistic
// options each carry a decision the plan should own:
//
//   - A commercial database: a licence agreement and an update pipeline.
//   - A free database: attribution terms and coarser data.
//   - An online lookup API: **every user's login IP sent to a third party**,
//     which is not acceptable for an identity provider under any terms.
//
// So detection sits behind Locator, this file reads the MaxMind DB *format* —
// which several free and commercial databases publish in — and **no database
// ships with the service**. An operator supplies one. With none configured,
// every lookup answers "unknown", new-location and travel detection simply do
// not fire, and new-device detection still works.

// Locator resolves an IP to a coarse location.
type Locator interface {
	// Locate returns the location, or the zero Location when there is none.
	// It never fails: a lookup that cannot answer is the same, to detection, as
	// an IP the data does not know.
	Locate(ip string) Location
}

// Unknown is the Locator a deployment gets with no database configured.
//
// Named rather than nil, so a caller never has to remember a nil check — and so
// "location detection is off" is a value somebody can see in the wiring instead
// of an absence they have to infer.
type Unknown struct{}

func (Unknown) Locate(string) Location { return Location{} }

// FileLocator reads a MaxMind-format database from disk.
type FileLocator struct {
	reader *maxminddb.Reader
}

// OpenLocator opens a database file.
//
// A file that cannot be opened is an ERROR at startup rather than a silent
// fallback to Unknown. An operator who configured a path expects location
// detection, and the difference between "on" and "quietly off because the path
// had a typo" should be visible on the first line of the log, not discovered
// the day somebody asks why impossible travel never fires.
func OpenLocator(path string) (*FileLocator, error) {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("anomaly: opening the geolocation database %q: %w", path, err)
	}
	return &FileLocator{reader: reader}, nil
}

// Close releases the database.
func (f *FileLocator) Close() error {
	if f == nil || f.reader == nil {
		return nil
	}
	return f.reader.Close()
}

// record is the subset of a GeoLite2-City-shaped record this service reads.
//
// City, country and a point with its accuracy radius — nothing finer. The
// database may hold postal codes and subdivisions; decoding them would be
// collecting precision this feature does not need and the card warns against.
type record struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Location struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
}

// Locate resolves one IP.
func (f *FileLocator) Locate(ip string) Location {
	if f == nil || f.reader == nil {
		return Location{}
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return Location{}
	}

	// A private, loopback or link-local address has no public location, and a
	// database lookup for one returns either nothing or somebody's guess. Both
	// would be noise: every login through a corporate proxy or a local
	// development stack would look like it came from nowhere, or from one
	// arbitrary place.
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return Location{}
	}

	result := f.reader.Lookup(addr.Unmap())
	if !result.Found() {
		return Location{}
	}

	var rec record
	if err := result.Decode(&rec); err != nil {
		return Location{}
	}
	if rec.Country.ISOCode == "" {
		return Location{}
	}

	return Location{
		City:       rec.City.Names["en"],
		Country:    rec.Country.ISOCode,
		Latitude:   rec.Location.Latitude,
		Longitude:  rec.Location.Longitude,
		AccuracyKM: float64(rec.Location.AccuracyRadius),
	}
}

// ErrNoLocator is returned where a configured locator was required and absent.
var ErrNoLocator = errors.New("anomaly: no geolocation database is configured")
