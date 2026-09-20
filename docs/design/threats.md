# Threat intelligence and query pattern analysis

Aegis answers every query the network makes, so it sees what malware does on
that network. This records what the analyser watches for, why each signal needs
two factors before it speaks, and where the findings land. It is the design
record for #26, and the note on that issue is the constraint that shaped it:
a detector an operator learns to ignore is worse than no detector.

## The decision

The analyser is an observer. It sits beside the query log on the
`dns.Observer` seam, funnels decisions through one buffered channel, and one
goroutine owns every detection window, so the resolver worker running
`Observe` never waits and never touches shared state. Findings are annotations:
nothing in this package blocks a query, changes a verdict, or feeds the filter.
The engine stays pure tables; intelligence stays a reading of traffic.

Enrichment from threat feeds happens in the query log's writer, not on the
resolver worker and not in the browser. The writer stamps each row with the
kind a feed assigns to the queried name, read from an immutable index behind
one atomic pointer, the publish-once swap the filter engine uses.

## The signals, and why each has two factors

| Behaviour | Shape factor | Volume factor | Both gate the finding |
| --- | --- | --- | --- |
| DGA | entropy of the registered label at or above 3.2 bits per character, label at least 6 characters | 15 distinct such domains from one client in 10 minutes | yes |
| Tunnelling | labels of 30 characters or more under one zone | 20 distinct such labels in 10 minutes, and at least half the zone's queries are TXT or NULL | yes |
| Beaconing | the same name queried with a coefficient of variation at or below 0.2, mean interval between 15 seconds and 2 hours | 8 completed intervals | yes |

Shape alone would flag the long content hashes CDNs hand out, so the entropy
test scores the label the domain's owner registered, never a subdomain:
`a3f9c2e5b8d7f1a2.cloudfront.net` scores `cloudfront`. Volume alone would flag
a curious user. Periodicity alone would flag every captive-portal check on the
network, so the beacon detector also keeps an exact-match list of names whose
regular repetition is the platform checking connectivity:
`captive.apple.com`, `connectivitycheck.gstatic.com`, `msftconnecttest.com`,
`pool.ntp.org`, and friends. A finding silences its client and behaviour for
one window, so a beacon that keeps beaconing does not flood the panel.

Findings always carry evidence: a sample of the generated names, the encoded
labels of a tunnel, the interval list of a beacon. An operator who doubts a
finding can check it against the names that earned it.

## Threat feeds

A feed is a fetched list of classified domains, one per line with an optional
kind, defaulting to `malware` (`docs/design/list-formats.md` would call this
the one format for the job). Feeds configure like blocklist sources: a
`--threat-feed name=url` flag seeds them on first boot, the API manages them at
`/api/v1/threats/feeds`, and a refresh fetches, parses at the boundary, and
replaces one feed's rows in a transaction. A feed that fails to fetch keeps the
rows it already published, the way a failed blocklist fetch keeps serving the
stored list.

Feed classification never blocks. A name a feed claims is stamped on its log
row and shown as a badge, because an operator who sees `malware` beside a
blocked name knows the list that caught it was not a lucky match.

## The units and their checks

1. **Detection.** `internal/threat` holds the detector and its windows. The
   check is the issue's done-when: a known DGA-shaped sequence fed through the
   detector produces one finding with the names as evidence, and 14 names
   produce none.
2. **Storage.** Findings live on the log database handle with their own
   retention bound of 5000 rows; feeds and classified domains live on the
   configuration handle, feeds cascading to their domains. The check is a
   round trip through a real SQLite file.
3. **Wiring.** The service observes beside the query log and records batches
   on the same one-second cadence. The check feeds 16 real UDP queries through
   the built binary and reads the finding back from the API.
4. **Feeds.** Sync, index, API, and log stamping. The check adds a feed over
   the API from a file, queries the classified name, and finds `threat` on the
   logged row.
5. **Dashboard.** A threat panel above the stats and a badge in the query log.
   The check drives the built binary over CDP and reads both from the page.

## What is not built

**No per-feed refresh schedules or etags.** Feeds refresh on the source
interval; a feed that wants its own cadence needs the columns and the diff
preview the blocklist sources grew.

**No automatic response.** A finding never writes a rule. Turning a finding
into a block is the operator's click on the log row, because a detector with
authority eventually acts on a false positive.

**No scoring or machine-learned classification.** Every signal above can be
explained to an operator in one sentence. A score cannot.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| observer service, one goroutine owns the windows | chosen | the hot path stays untouched and the state needs no locks |
| analysis inside the decider or handler | windows checked per query on the resolver worker | rejected, per-client state on the hot path wants locks, and intelligence is not policy |
| feed enrichment in the handler's decision | the decision carries its threat kind | rejected, the decision is the filter's answer, and a metadata lookup there would run per query forever instead of per flush |
| feed enrichment in the browser | the dashboard joins the log against a feed endpoint | rejected, two sources of truth and the boundary parse moves into the client |
