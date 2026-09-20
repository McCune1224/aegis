// Package threat watches the decision stream for query patterns that betray
// malware: generated domains, DNS tunnelling, and beaconing. It is an observer,
// so it never blocks and never changes a verdict; findings are evidence for the
// operator, recorded with the names that triggered them. Every signal is
// two-factor on purpose, because a detector the operator learns to ignore is
// worse than no detector.
package threat

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/internal/dns"
)

// Kind names the behaviour a finding describes.
type Kind string

const (
	DGA    Kind = "dga"
	Tunnel Kind = "tunnel"
	Beacon Kind = "beacon"
)

const (
	// dgaEntropy is the per-character Shannon entropy a generated label clears
	// and human labels do not. English words land near 2.8; random base36 near
	// 5.2. The threshold sits between, and the volume factor below is what
	// keeps the marginal names from mattering.
	dgaEntropy = 3.2

	// dgaMinLabel is the shortest label the entropy test trusts. Short random
	// labels are common in legitimate short links and record hashes.
	dgaMinLabel = 6

	// dgaThreshold is how many distinct generated-looking domains one client
	// must ask for inside the window before the analyser speaks. A DGA beacon
	// works through a list; a human touches a handful of odd names a day.
	dgaThreshold = 15

	// dgaWindow is how long one client's shaped-domain tally is remembered,
	// and how long a finding keeps the detector quiet about that client.
	dgaWindow = 10 * time.Minute

	// tunnelThreshold is how many distinct long labels under one zone make a
	// tunnel. DKIM selectors and SPF lookups stay in single digits.
	tunnelThreshold = 20

	// tunnelLabelLen is the encoded-payload length a legitimate label never
	// carries.
	tunnelLabelLen = 30

	// tunnelWindow is how long a zone's label tally is remembered, and the
	// quiet period after a zone is flagged.
	tunnelWindow = 10 * time.Minute

	// beaconObservations is how many intervals a client must repeat one name
	// before periodicity counts. NTP and captive-portal checks are the reason
	// the benign list exists, not the reason this number is high.
	beaconObservations = 8

	// beaconCV is the coefficient of variation under which intervals count as
	// regular. Real malware timers jitter a few percent; retries scatter.
	beaconCV = 0.2

	// beaconInterval bounds the mean interval worth calling a beacon: slower
	// than retries, faster than a cron nobody schedules for malware.
	beaconMinInterval = 15 * time.Second
	beaconMaxInterval = 2 * time.Hour

	// beaconWindow is the quiet period after a name is flagged.
	beaconWindow = 3 * time.Hour

	// maxClients bounds the detector's memory. A client unseen this long is
	// dropped when the map overflows.
	maxClients = 4096
	clientTTL  = time.Hour
)

// benignPeriodicity lists the names whose regular repetition is the platform
// checking connectivity, not malware phoning home. Exact match.
var benignPeriodicity = map[string]bool{
	"captive.apple.com":             true,
	"connectivitycheck.gstatic.com": true,
	"connectivitycheck.android.com": true,
	"msftconnecttest.com":           true,
	"dns.msftncsi.com":              true,
	"nmcheck.gnome.org":             true,
	"pool.ntp.org":                  true,
	"time.apple.com":                true,
	"time.windows.com":              true,
}

// Finding is one detected pattern, with the evidence an operator needs to
// decide whether to act.
type Finding struct {
	Time     time.Time
	Client   string
	Kind     Kind
	Summary  string
	Evidence []string
}

// Detector reduces a decision stream to findings. Its state is per client and
// its windows are per behaviour, so one noisy device cannot flag another.
type Detector struct {
	now     func() time.Time
	clients map[string]*clientState
}

// Option configures a Detector.
type Option func(*Detector)

// WithClock replaces the wall clock, which is how tests advance time.
func WithClock(now func() time.Time) Option {
	return func(d *Detector) { d.now = now }
}

// NewDetector returns a Detector with no history.
func NewDetector(options ...Option) *Detector {
	d := &Detector{now: time.Now, clients: make(map[string]*clientState)}
	for _, option := range options {
		option(d)
	}
	return d
}

type clientState struct {
	lastSeen    time.Time
	dga         map[string]time.Time
	dgaFlagged  time.Time
	zones       map[string]*zoneState
	zoneFlagged map[string]time.Time
	beacons     map[string]*beaconState
}

type zoneState struct {
	labels map[string]time.Time
	txt    int
	total  int
	start  time.Time
}

type beaconState struct {
	times   []time.Time
	flagged time.Time
}

// Observe folds one decision into the windows and returns the findings it
// completed. Findings are per decision, never duplicates of one already
// emitted inside the same window.
func (d *Detector) Observe(decision dns.Decision) []Finding {
	now := d.now()
	key := string(decision.Client)
	if key == "" {
		key = decision.Address.Unmap().String()
	}
	state := d.clients[key]
	if state == nil {
		if len(d.clients) >= maxClients {
			d.expired(now)
		}
		state = &clientState{
			dga:         make(map[string]time.Time),
			zones:       make(map[string]*zoneState),
			zoneFlagged: make(map[string]time.Time),
			beacons:     make(map[string]*beaconState),
		}
		d.clients[key] = state
	}
	state.lastSeen = now

	var findings []Finding
	for _, observe := range []func(*clientState, string, dns.Decision, time.Time) (Finding, bool){
		observeDGA,
		observeTunnel,
		observeBeacon,
	} {
		if finding, flagged := observe(state, key, decision, now); flagged {
			findings = append(findings, finding)
		}
	}
	return findings
}

// expired drops clients the detector has not heard from, when memory is the
// alternative. Called under the map-size cap only.
func (d *Detector) expired(now time.Time) {
	for key, state := range d.clients {
		if now.Sub(state.lastSeen) > clientTTL {
			delete(d.clients, key)
		}
	}
}

// observeDGA counts distinct generated-looking registrable domains for one
// client. Shape alone never flags: the volume factor is the second gate.
func observeDGA(state *clientState, key string, decision dns.Decision, now time.Time) (Finding, bool) {
	if !state.dgaFlagged.IsZero() {
		if now.Sub(state.dgaFlagged) < dgaWindow {
			return Finding{}, false
		}
		state.dgaFlagged = time.Time{}
	}
	registrable := registrableDomain(decision.Name.String())
	label := ownedLabel(registrable)
	if !dgaShape(label) {
		return Finding{}, false
	}
	for name, seen := range state.dga {
		if now.Sub(seen) > dgaWindow {
			delete(state.dga, name)
		}
	}
	state.dga[registrable] = now
	if len(state.dga) <= dgaThreshold {
		return Finding{}, false
	}
	finding := Finding{
		Time:     now,
		Client:   key,
		Kind:     DGA,
		Summary:  "asked for " + strconv.Itoa(len(state.dga)) + " generated-looking domains in " + dgaWindow.String(),
		Evidence: sampleNames(state.dga, 10),
	}
	state.dga = make(map[string]time.Time)
	state.dgaFlagged = now
	return finding, true
}

// observeTunnel counts distinct long labels under one zone. The query type is
// the second gate: tunnels ride TXT and NULL, and ordinary lookups do not.
func observeTunnel(state *clientState, key string, decision dns.Decision, now time.Time) (Finding, bool) {
	name := decision.Name.String()
	labels := strings.Split(name, ".")
	if len(labels) < 3 {
		return Finding{}, false
	}
	zone := registrableDomain(name)
	if flagged, active := state.zoneFlagged[zone]; active {
		if now.Sub(flagged) < tunnelWindow {
			return Finding{}, false
		}
		delete(state.zoneFlagged, zone)
	}
	payload := longestLabel(labels[:len(labels)-2])
	if len(payload) < tunnelLabelLen {
		return Finding{}, false
	}
	tally := state.zones[zone]
	if tally == nil || now.Sub(tally.start) > tunnelWindow {
		tally = &zoneState{labels: make(map[string]time.Time), start: now}
		state.zones[zone] = tally
	}
	tally.labels[payload] = now
	tally.total++
	if decision.Type == "TXT" || decision.Type == "NULL" {
		tally.txt++
	}
	encoded := float64(tally.txt) / float64(tally.total)
	if len(tally.labels) <= tunnelThreshold || encoded < 0.5 {
		return Finding{}, false
	}
	share := strconv.Itoa(int(encoded*100)) + "%"
	finding := Finding{
		Time:   now,
		Client: key,
		Kind:   Tunnel,
		Summary: "carried " + strconv.Itoa(len(tally.labels)) + " distinct encoded labels under " + zone +
			" with " + share + " TXT or NULL queries",
		Evidence: sampleNames(tally.labels, 10),
	}
	delete(state.zones, zone)
	state.zoneFlagged[zone] = now
	return finding, true
}

// observeBeacon reads the client's interval history for one name and flags the
// regularity. Names the whole internet polls on a timer are exempt.
func observeBeacon(state *clientState, key string, decision dns.Decision, now time.Time) (Finding, bool) {
	name := decision.Name.String()
	if benignPeriodicity[name] {
		return Finding{}, false
	}
	beacon := state.beacons[name]
	if beacon == nil {
		beacon = &beaconState{}
		state.beacons[name] = beacon
	}
	if !beacon.flagged.IsZero() {
		if now.Sub(beacon.flagged) < beaconWindow {
			return Finding{}, false
		}
		beacon.flagged = time.Time{}
	}
	beacon.times = append(beacon.times, now)
	if len(beacon.times) < beaconObservations+1 {
		return Finding{}, false
	}
	beacon.times = beacon.times[len(beacon.times)-beaconObservations-1:]
	intervals := make([]float64, 0, beaconObservations)
	for i := 1; i < len(beacon.times); i++ {
		gap := beacon.times[i].Sub(beacon.times[i-1])
		if gap < beaconMinInterval || gap > beaconMaxInterval {
			beacon.times = beacon.times[len(beacon.times)-1:]
			return Finding{}, false
		}
		intervals = append(intervals, gap.Seconds())
	}
	mean, cv := variation(intervals)
	if cv > beaconCV {
		return Finding{}, false
	}
	finding := Finding{
		Time:     now,
		Client:   key,
		Kind:     Beacon,
		Summary:  "queried " + name + " " + strconv.Itoa(len(intervals)) + " times at " + strconv.Itoa(int(mean)) + "s mean intervals with " + strconv.Itoa(int(cv*100)) + "% jitter",
		Evidence: sampleIntervals(intervals),
	}
	beacon.times = beacon.times[len(beacon.times)-1:]
	beacon.flagged = now
	return finding, true
}

// entropy is the Shannon entropy of the string's character distribution in
// bits per character.
func entropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	total := float64(len(s))
	sum := 0.0
	for _, count := range counts {
		p := float64(count) / total
		sum -= p * math.Log2(p)
	}
	return sum
}

// dgaShape reports whether a label looks machine-generated. It scores the
// registered label, never a subdomain, so the content hashes CDNs put under
// their own domains do not score.
func dgaShape(label string) bool {
	return len(label) >= dgaMinLabel && entropy(label) >= dgaEntropy
}

// registrableDomain keeps the last two labels, an approximation of the
// registrable domain that treats every TLD as one label.
func registrableDomain(name string) string {
	labels := strings.Split(name, ".")
	if len(labels) <= 2 {
		return name
	}
	return labels[len(labels)-2] + "." + labels[len(labels)-1]
}

// ownedLabel picks the label of a registrable domain its owner chose: the
// label left of the TLD pair. It is the one entropy scores.
func ownedLabel(registrable string) string {
	labels := strings.Split(registrable, ".")
	if len(labels) < 3 {
		return registrable
	}
	return labels[len(labels)-3]
}

func longestLabel(labels []string) string {
	longest := ""
	for _, label := range labels {
		if len(label) > len(longest) {
			longest = label
		}
	}
	return longest
}

func variation(intervals []float64) (mean, cv float64) {
	sum := 0.0
	for _, interval := range intervals {
		sum += interval
	}
	mean = sum / float64(len(intervals))
	if mean == 0 {
		return mean, 0
	}
	spread := 0.0
	for _, interval := range intervals {
		spread += (interval - mean) * (interval - mean)
	}
	sd := math.Sqrt(spread / float64(len(intervals)))
	return mean, sd / mean
}

func sampleNames(names map[string]time.Time, limit int) []string {
	list := make([]string, 0, len(names))
	for name := range names {
		list = append(list, name)
	}
	sort.Strings(list)
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}

func sampleIntervals(intervals []float64) []string {
	samples := make([]string, 0, len(intervals))
	for _, interval := range intervals {
		samples = append(samples, strconv.Itoa(int(interval))+"s")
	}
	return samples
}
