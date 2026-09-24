package threat

import (
	"net/netip"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/dns"
	"aegis/internal/filter"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func decision(clock *fakeClock, client filter.ClientKey, name, qtype string) dns.Decision {
	parsed, err := filter.ParseDomain(name)
	if err != nil {
		panic(err)
	}
	address := netip.MustParseAddr("10.9.9.44")
	return dns.Decision{
		Time:    clock.Now(),
		Address: address,
		Name:    parsed,
		Type:    qtype,
		Action:  filter.ActionAllow,
		Client:  client,
	}
}

const dgaLabel = "xkvzwmqpoierutti"

func dgaName(sequence int) string {
	suffix := []rune("abcdefghijklmnopqrstuvwxyz")
	name := []rune(dgaLabel)
	name[sequence%len(name)] = suffix[sequence/len(name)%len(suffix)]
	name[(sequence*7)%len(name)] = suffix[(sequence*3)%len(suffix)]
	return string(name) + ".biz"
}

func TestEntropyRanksGeneratedLabelsAboveHumanOnes(t *testing.T) {
	human := entropy("google")
	generated := entropy(dgaLabel)
	if human >= generated {
		t.Fatalf("entropy(google)=%f should rank below entropy(%s)=%f", human, dgaLabel, generated)
	}
}

func TestRegistrableDomainDropsSubdomains(t *testing.T) {
	cases := map[string]string{
		"a.b.example.com":     "example.com",
		"example.com":         "example.com",
		"localhost":           "localhost",
		"hash.cloudfront.net": "cloudfront.net",
	}
	for name, want := range cases {
		if got := registrableDomain(name); got != want {
			t.Fatalf("registrable(%q)=%q, want %q", name, got, want)
		}
	}
}

func TestDGAShapeSeeksRandomLabelsAndSkipsHumanOnes(t *testing.T) {
	for _, name := range []string{"google", "wikipedia", "cloudfront", "abcd12", ""} {
		if dgaShape(name) {
			t.Fatalf("dgaShape(%q) should be false", name)
		}
	}
	for _, name := range []string{dgaLabel, "a3f9c2e5b8d7f1a2", "qvwxzkjmhp"} {
		if !dgaShape(name) {
			t.Fatalf("dgaShape(%q) should be true", name)
		}
	}
}

func TestDetectorFlagsADGASequenceWithEvidence(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i < dgaThreshold; i++ {
		findings := detector.Observe(decision(clock, "phone", dgaName(i), "A"))
		if len(findings) != 0 {
			t.Fatalf("finding before the sequence completed: %+v", findings)
		}
		clock.advance(time.Second)
	}
	findings := detector.Observe(decision(clock, "phone", dgaName(dgaThreshold), "A"))
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}
	finding := findings[0]
	if finding.Kind != DGA {
		t.Fatalf("kind %q, want dga", finding.Kind)
	}
	if finding.Client != "phone" {
		t.Fatalf("client %q, want phone", finding.Client)
	}
	if len(finding.Evidence) == 0 {
		t.Fatal("the finding carries no evidence")
	}
	joined := strings.Join(finding.Evidence, " ")
	if !strings.Contains(joined, ".biz") {
		t.Fatalf("evidence %q does not name the generated names", joined)
	}
}

func TestEvidenceCapsAtTenNamesAndKeepsTheAlphabeticalHead(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	asked := make([]string, 0, dgaThreshold+1)
	var findings []Finding
	for i := 0; len(findings) == 0; i++ {
		name := dgaName(i)
		asked = append(asked, name)
		findings = detector.Observe(decision(clock, "phone", name, "A"))
		if len(findings) == 0 {
			clock.advance(time.Second)
		}
	}
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}

	want := append([]string(nil), asked...)
	sort.Strings(want)
	require.Equal(t, want[:10], findings[0].Evidence, "evidence is the first ten of the sorted names")
}

func TestDetectorNeedsBothShapeAndVolume(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i < dgaThreshold; i++ {
		detector.Observe(decision(clock, "phone", "google.com", "A"))
		clock.advance(time.Second)
	}
	if findings := detector.Observe(decision(clock, "phone", "google.com", "A")); len(findings) != 0 {
		t.Fatalf("repeating human names flagged: %+v", findings)
	}

	oneUnder := NewDetector(WithClock(clock.Now))
	for i := 0; i < dgaThreshold-1; i++ {
		oneUnder.Observe(decision(clock, "phone", dgaName(i), "A"))
		clock.advance(time.Second)
	}
	if findings := oneUnder.Observe(decision(clock, "phone", dgaName(dgaThreshold), "A")); len(findings) != 0 {
		t.Fatalf("volume at the threshold, not past it, flagged: %+v", findings)
	}
}

func TestDetectorScopesWindowsPerClient(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i < dgaThreshold/2; i++ {
		detector.Observe(decision(clock, "phone", dgaName(i), "A"))
		detector.Observe(decision(clock, "laptop", dgaName(i+100), "A"))
		clock.advance(time.Second)
	}
	for i := dgaThreshold / 2; i < dgaThreshold; i++ {
		if findings := detector.Observe(decision(clock, "phone", dgaName(i), "A")); len(findings) != 0 {
			t.Fatalf("two clients splitting the volume flagged early: %+v", findings)
		}
		if findings := detector.Observe(decision(clock, "laptop", dgaName(i+100), "A")); len(findings) != 0 {
			t.Fatalf("two clients splitting the volume flagged early: %+v", findings)
		}
		clock.advance(time.Second)
	}
}

func TestDetectorSuppressesRepeatFindingsUntilTheWindowPasses(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i < dgaThreshold; i++ {
		detector.Observe(decision(clock, "phone", dgaName(i), "A"))
		clock.advance(time.Second)
	}
	if findings := detector.Observe(decision(clock, "phone", dgaName(dgaThreshold+1), "A")); len(findings) != 1 {
		t.Fatalf("want the first finding, got %d", len(findings))
	}
	clock.advance(time.Minute)
	if findings := detector.Observe(decision(clock, "phone", dgaName(dgaThreshold+2), "A")); len(findings) != 0 {
		t.Fatalf("the window did not quiet the detector: %+v", findings)
	}
	clock.advance(dgaWindow)
	for i := 0; i < dgaThreshold; i++ {
		detector.Observe(decision(clock, "phone", dgaName(i+200), "A"))
		clock.advance(time.Second)
	}
	if findings := detector.Observe(decision(clock, "phone", dgaName(300), "A")); len(findings) != 1 {
		t.Fatalf("a fresh sequence after the window was not flagged: %d findings", len(findings))
	}
}

func TestDetectorFlagsTunnellingUnderOneZone(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i < tunnelThreshold; i++ {
		label := strings.Repeat("abcdefgh", 4) + string(rune('a'+i%26))
		detector.Observe(decision(clock, "phone", label+".exfil.example", "TXT"))
		clock.advance(time.Second)
	}
	findings := detector.Observe(decision(clock, "phone", strings.Repeat("abcdefgh", 4)+"zz.exfil.example", "TXT"))
	if len(findings) != 1 {
		t.Fatalf("want one tunnelling finding, got %d", len(findings))
	}
	if findings[0].Kind != Tunnel {
		t.Fatalf("kind %q, want tunnel", findings[0].Kind)
	}
	if !strings.Contains(findings[0].Summary, "exfil.example") {
		t.Fatalf("summary %q does not name the zone", findings[0].Summary)
	}
	if len(findings[0].Evidence) == 0 {
		t.Fatal("the tunnelling finding carries no evidence")
	}
}

func TestDetectorNeedsTXTForTunnelling(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i <= tunnelThreshold; i++ {
		label := strings.Repeat("abcdefgh", 4) + string(rune('a'+i%26))
		detector.Observe(decision(clock, "phone", label+".exfil.example", "A"))
		clock.advance(time.Second)
	}
	if findings := detector.Observe(decision(clock, "phone", strings.Repeat("abcdefgh", 4)+"zz.exfil.example", "A")); len(findings) != 0 {
		t.Fatalf("A-only long labels flagged: %+v", findings)
	}
}

func TestDetectorFlagsRegularBeaconingAndSkipsJitter(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	regular := NewDetector(WithClock(clock.Now))
	for i := 0; i < beaconObservations; i++ {
		if findings := regular.Observe(decision(clock, "phone", "c2.example.biz", "A")); len(findings) != 0 {
			t.Fatalf("flagged after only %d observations: %+v", i+1, findings)
		}
		clock.advance(time.Minute)
	}
	findings := regular.Observe(decision(clock, "phone", "c2.example.biz", "A"))
	if len(findings) != 1 || findings[0].Kind != Beacon {
		t.Fatalf("want one beacon finding, got %+v", findings)
	}

	jittered := NewDetector(WithClock(clock.Now))
	for i := 0; i < beaconObservations; i++ {
		jittered.Observe(decision(clock, "phone", "c2.example.biz", "A"))
		clock.advance(time.Duration(20+i*22) * time.Second)
	}
	if findings := jittered.Observe(decision(clock, "phone", "c2.example.biz", "A")); len(findings) != 0 {
		t.Fatalf("jittered intervals flagged: %+v", findings)
	}
}

func TestDetectorSparesTheBenignPeriodicityList(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)}
	detector := NewDetector(WithClock(clock.Now))

	for i := 0; i <= beaconObservations+4; i++ {
		if findings := detector.Observe(decision(clock, "phone", "captive.apple.com", "A")); len(findings) != 0 {
			t.Fatalf("the captive portal check was flagged: %+v", findings)
		}
		clock.advance(45 * time.Second)
	}
}
