package anomaly

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// The file-backed locator, against a database written by the test (P3-08).
//
// **No real geolocation data is involved.** The test builds a two-network MMDB
// file whose contents are exactly what the assertions expect, so there is no
// licence to accept, no database to download, and no fixture that goes stale
// when a provider moves an address block. It tests the part this service owns —
// decoding a record into a Location and refusing addresses with no public
// location — rather than the accuracy of anybody's data.

// TEST-NET documentation ranges (RFC 5737), so no real address is implied.
const (
	jakartaIP = "198.51.100.10"
	londonIP  = "203.0.113.20"
	unmapped  = "192.0.2.30"
)

func writeDatabase(t *testing.T) string {
	t.Helper()

	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-City",
		// The documentation ranges are reserved, and the writer refuses
		// reserved networks unless told otherwise.
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}

	insert := func(cidr, city, country string, lat, lon float64, radius uint16) {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatalf("parsing %s: %v", cidr, err)
		}
		if err := tree.Insert(network, mmdbtype.Map{
			"city": mmdbtype.Map{
				"names": mmdbtype.Map{"en": mmdbtype.String(city)},
			},
			"country": mmdbtype.Map{"iso_code": mmdbtype.String(country)},
			"location": mmdbtype.Map{
				"latitude":        mmdbtype.Float64(lat),
				"longitude":       mmdbtype.Float64(lon),
				"accuracy_radius": mmdbtype.Uint16(radius),
			},
		}); err != nil {
			t.Fatalf("inserting %s: %v", cidr, err)
		}
	}

	insert("198.51.100.0/24", "Jakarta", "ID", -6.2088, 106.8456, 20)
	insert("203.0.113.0/24", "London", "GB", 51.5074, -0.1278, 50)

	path := filepath.Join(t.TempDir(), "test-city.mmdb")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the database file: %v", err)
	}
	if _, err := tree.WriteTo(file); err != nil {
		_ = file.Close()
		t.Fatalf("writing the database: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}
	return path
}

func TestTheLocatorDecodesARecord(t *testing.T) {
	locator, err := OpenLocator(writeDatabase(t))
	if err != nil {
		t.Fatalf("OpenLocator: %v", err)
	}
	t.Cleanup(func() { _ = locator.Close() })

	got := locator.Locate(jakartaIP)

	if got.City != "Jakarta" || got.Country != "ID" {
		t.Errorf("Locate(%s) = %s/%s, want Jakarta/ID", jakartaIP, got.City, got.Country)
	}
	if got.AccuracyKM != 20 {
		t.Errorf("AccuracyKM = %.0f, want 20 — travel detection subtracts it", got.AccuracyKM)
	}
	if got.Latitude == 0 || got.Longitude == 0 {
		t.Errorf("the coordinates were not decoded: %+v", got)
	}
}

// The decoded locations feed impossible travel correctly, end to end.
func TestDecodedLocationsDriveTravelDetection(t *testing.T) {
	locator, err := OpenLocator(writeDatabase(t))
	if err != nil {
		t.Fatalf("OpenLocator: %v", err)
	}
	t.Cleanup(func() { _ = locator.Close() })

	a, b := locator.Locate(jakartaIP), locator.Locate(londonIP)
	if !ImpossibleTravel(a, b, 0) {
		t.Errorf("Jakarta then London at the same instant was not impossible: %+v → %+v", a, b)
	}
}

// An address the database does not hold is unknown, not guessed.
func TestAnUnknownAddressIsUnknown(t *testing.T) {
	locator, err := OpenLocator(writeDatabase(t))
	if err != nil {
		t.Fatalf("OpenLocator: %v", err)
	}
	t.Cleanup(func() { _ = locator.Close() })

	if got := locator.Locate(unmapped); got.Known() {
		t.Errorf("an address the database does not hold resolved to %+v", got)
	}
}

// Private, loopback and malformed addresses have no public location, and are
// never looked up — otherwise every login through a corporate proxy or a local
// stack would look like it came from one arbitrary place.
func TestAddressesWithNoPublicLocationAreUnknown(t *testing.T) {
	locator, err := OpenLocator(writeDatabase(t))
	if err != nil {
		t.Fatalf("OpenLocator: %v", err)
	}
	t.Cleanup(func() { _ = locator.Close() })

	for _, ip := range []string{
		"10.0.0.5", "192.168.1.1", "172.16.0.1", // private
		"127.0.0.1", "::1", // loopback
		"169.254.1.1", "fe80::1", // link-local
		"not an ip", "", // malformed
	} {
		if got := locator.Locate(ip); got.Known() {
			t.Errorf("Locate(%q) resolved to %+v", ip, got)
		}
	}
}

// An IPv4-mapped IPv6 address resolves as its IPv4 form, since that is how a
// dual-stack listener often reports a v4 client.
func TestAMappedAddressResolvesAsItsIPv4Form(t *testing.T) {
	locator, err := OpenLocator(writeDatabase(t))
	if err != nil {
		t.Fatalf("OpenLocator: %v", err)
	}
	t.Cleanup(func() { _ = locator.Close() })

	if got := locator.Locate("::ffff:" + jakartaIP); got.City != "Jakarta" {
		t.Errorf("a v4-mapped address resolved to %+v, want Jakarta", got)
	}
}

// A path that does not open is an error, not a silent fallback.
func TestAMissingDatabaseIsAnError(t *testing.T) {
	if _, err := OpenLocator(filepath.Join(t.TempDir(), "absent.mmdb")); err == nil {
		t.Error("opening a database that does not exist succeeded; location detection would be silently off")
	}
}

// The Unknown locator is what an unconfigured deployment gets, and it answers
// "unknown" for everything — which keeps new-device detection working.
func TestTheUnknownLocatorKnowsNothing(t *testing.T) {
	var l Locator = Unknown{}
	if l.Locate(jakartaIP).Known() {
		t.Error("the Unknown locator resolved an address")
	}
}

// A nil FileLocator answers unknown rather than panicking.
func TestANilLocatorIsUnknown(t *testing.T) {
	var f *FileLocator
	if f.Locate(jakartaIP).Known() {
		t.Error("a nil locator resolved an address")
	}
	if err := f.Close(); err != nil {
		t.Errorf("closing a nil locator: %v", err)
	}
}
