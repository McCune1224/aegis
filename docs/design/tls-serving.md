# Encrypted serving: DoT and DoH

This records the shape of the encrypted listeners and what they deliberately
leave out.

## The decision

DoT and DoH are transports, not pipeline. Both feed the same `Handler` the
plaintext listeners use: DoT is a `tls.NewListener` wrapped around the TCP
listener handed to `miekg/dns`, DoH is an `http.Server` whose handler unpacks
RFC 8484 wire messages and calls the same `Handle(ctx, req, clientAddr)`. One
pipeline means one set of verdicts, rewrites, cache semantics, and client
identity everywhere; a query answered differently because it arrived encrypted
would be a bug class the architecture invited.

The client address survives encryption untouched — a TLS conn's `RemoteAddr`
is still the peer's TCP address, and the HTTP request carries it in
`RemoteAddr` — so per-client policy and rate limits hold on encrypted
connections with no identity code per transport.

One certificate pair serves both listeners. AdGuard Home does the same: one
TLS settings block behind plain, DoT, and DoH. Certificates are
operator-supplied PEM files (`--tls-cert`, `--tls-key`), loaded in the same
parse-before-database block as every other flag, so a bad path dies before a
half-initialised store exists. The pair and the listener addresses are
validated together: listeners without a certificate, half a pair, or a pair
with no listener are all errors, not warnings.

DoH details that are not negotiable: GET with the unpadded base64url `dns`
parameter, POST with `application/dns-message`, 405 for other methods, 415
for a wrong media type, 400 for an unparseable message, and a 64KiB body cap
(no DNS message crosses that on any transport). The response carries
`Cache-Control: max-age=<shortest answer TTL>` per RFC 8484 section 5.1. The
server offers h2, so DoH clients get HTTP/2 without extra configuration; DoT
clients ignore the ALPN offer.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| shared handler | wrap each transport around one `Handle` | chosen |
| per-transport handlers | DoH gets its own decider path | rejected, two places to answer one query differently |
| ACME issuance | automatic certificates on first run | deferred, an operator pointing devices at the sinkhole already runs a PKI or accepts a self-signed pin; revisit when a home operator without one asks |
| one listener speaking DoT and DoH by sniffing | single port | rejected, two sockets is simpler and ports are free |

## Deferred

- **DoQ.** Needs quic-go, deferred in `docs/stack.md` until a reason exists.
  No browser ships it and no client on a home network defaults to it.
- **Certificate reload on write.** The pair is loaded at startup; rotating it
  means a restart. SIGHUP or an API reload is a small slice once someone runs
  a cert bot against it.
