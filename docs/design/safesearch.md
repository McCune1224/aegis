# SafeSearch

A profile can enforce safe search for a set of search engines. This records how
Aegis knows the safe hostnames, how the answer reaches the client, and where the
behaviour differs from AdGuard Home.

## The decision

Safe search is a per-profile set of engines. `internal/safesearch` holds the
engine catalog and, for each profile, a rewrite index built from the engines it
enabled. The DNS handler already follows a CNAME chain through the rewrite seam,
so a safe-search answer is an ordinary rewrite: `www.google.com` becomes a CNAME
to `forcesafesearch.google.com`, the target is then filtered and resolved
through the whole pipeline, and the client gets the safe host's real address.
Nothing pins an IP, and the query log records the safe host as the rule that
answered.

The engine catalog is the two lists AdGuard Home builds its own SafeSearch from,
copied into the binary at release time:

    engines_safe_search.txt  — Bing, Brave, DuckDuckGo, Ecosia, Google,
                               Pixabay, Qwant, Yandex
    youtube_safe_search.txt  — YouTube

The lists are parsed at first use, so the file content is the data and the
parser is tested against the real dialect. A rule is
`|host^$dnsrewrite=NOERROR;CNAME;safehost`; the single pipe anchors one host, and
the file groups its rules with `# Engine` comments, which is where an engine's
identity comes from. The YouTube list has no such header and is parsed under the
engine name it is given.

**The limitation, and why it is release-maintained.** An engine can change its
safe hostname. When one does, the previous host stops enforcing safe search
silently, because the old name no longer resolves to a filtered result. The list
a binary carries is as new as the release, and updating it is copying the two
files again; there is no operator setting to add an engine, because a wrong safe
host is worse than an absent one.

## The profile, not the resolver

`profile_safesearch` records which engines a profile enforces. `Runtime`
resolves a query's profile the same way `filter.RuleSet` resolves rule scope —
the client's profile, or the default profile for an address nothing claims — so
the rewrite table and the block rules cannot disagree about who is asking. The
filter exposes that resolution as `ProfileOf`, and it is the only place the
mapping lives.

An operator's own rewrite wins over a safe-search answer. The configured table
is consulted first, and only a miss falls through to the profile's engines, so
pinning `www.google.com` to an address is not fought by the safe-search list.

## Where Aegis diverges from AdGuard Home

**The switch is per profile, not global.** AdGuard Home has one SafeSearch
setting for the whole resolver. Aegis keys it on the profile, the same way
blocked services are keyed, so a household can enforce safe search for a child's
profile and leave the adults' profile alone. The global behaviour is the default
profile's set.

**The answer rides the general rewrite path.** AdGuard Home owns a dedicated
SafeSearch rewrite step. Aegis reuses the CNAME chain the handler already
follows, which means the safe host is filtered and forwarded like any other
rewrite target and shows up in the query log with no separate code path.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| embedded upstream lists, per-profile engines | parse the two files at load, one rewrite index per profile | chosen |
| runtime fetch of the two lists | refresh like blocklist sources | rejected, the safe hostnames are documented and stable, and the issue asks for a release-maintained list; the cost of being wrong is silent non-enforcement |
| a hand-written Go table | engine → host → safe host literal | rejected, it duplicates the upstream dialect by hand and drifts from the file an operator can check |
| pin the safe host's address | answer with an A record | rejected, the address changes and a pinned one ages into a wrong answer |
