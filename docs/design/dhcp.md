# DHCP, MAC identity, and discovery

Aegis can serve DHCPv4 from the same pool the DNS listener lives in, so a device
that asks for an address is also told to ask Aegis to resolve. This records the
shape and the decisions, including the one that is not built yet. It is the
design record for #27.

## Why a lease is not configuration

Identity was by address: a device that changed address lost its policy, and a
new device was invisible until someone configured it. A lease closes both gaps,
and it is the reason the lease table is separate from the client table.

`leases` is not configuration. A row appears when an address is handed out,
changes when the device renews, and goes when the lease ends or the device gives
it back. The sweep removes the ended ones on a timer, which is a story none of
the configuration tables have. `client_macs` is configuration: it says which
device an operator has decided is `tablet`, and it survives every lease the
device takes.

The two meet in `internal/client`. `Resolver.Select` resolves a request in one
order, and the order is the whole design:

1. an exact address selector, because an operator who pinned an address meant it;
2. the hardware address, either from the request or from the lease that holds the
   address, because that is the identity a device cannot change;
3. the longest matching prefix, which is a whole network sharing a policy.

`Dynamic` is step two's other half: the DHCP server writes address-to-device as
it grants, and the DNS path reads it. It is a small mutex-guarded map rather than
a database read on every query, and it is shared across reloads because a lease
outlives a configuration change.

## The wire

`internal/dhcp` parses DHCPv4 at the boundary: `Parse` returns a typed `Message`,
and `Reply.Marshal` writes one. Everything after the fixed header is options,
and an option that runs past the end of the packet is an error rather than a
truncation, because a short read answers the wrong client. The all-zero address
fields become the zero address rather than the unspecified one, so `ciaddr` and
`giaddr` mean "not set" instead of "0.0.0.0" at every call site.

A reply goes to the relay when `giaddr` is set, to the client's own address when
the request came from one, and to the broadcast address when it did not — which
is the only address a client with no interface configured can hear. Sending to
the source address when there is one is also what makes a loopback test possible
without a privileged port.

The server commits the lease on the offer, not only on the ack, so two devices
asking at once cannot be promised the same address. An exhausted pool answers
nothing, which makes the client try again instead of accepting a reply that
grants it nothing.

The listen address is usually the wildcard, `0.0.0.0:67`, because a client with
no address sends to the limited broadcast address and a socket bound to one
unicast address never receives a broadcast. A server that only talks to relays
can bind a unicast address instead. The reply itself goes to the limited
broadcast address, which has no route, so the server names the interface the
request arrived on through the IPv4 control message; without that the kernel has
nothing to send it out of. A platform or socket that cannot name the interface
falls back to whatever route the kernel picks.

## Discovery

A lease whose device no selector claims is recorded in `discoveries`. That is
the whole prompt: the dashboard shows the address, the hardware address, and the
hostname the device sent, and the operator either names it — which creates a
client with the MAC and the address — or dismisses it. The record keeps its first
sighting across renewals, so the prompt can say how long the device has been
around, and the server deletes it the next time a claimed device renews.

The device is judged by the combined selector, address and hardware address
together, so the same resolution that decides a policy decides whether an
operator needs to be asked.

## What is not built

**No DHCPv6, and no DHCP options an operator can set.** The pool, the router,
and the resolver list come from flags at boot. AdGuard Home configures DHCP in
its web interface with its own stored settings; Aegis takes flags, which is
enough to serve a network and not enough to reconfigure it from the dashboard.
That is the next slice, and the store shape is already there to hold it.

**No static leases.** An operator who wants one device to always get one address
can pin the address as a client address selector, and the server will not hand
that address to anyone else because the pool is the only source of addresses and
the pinned address is outside it. A static lease inside the pool is a later
slice.

**No lease reading from an existing server.** The pool is Aegis's own. Reading
someone else's leases needs a foreign schema and is a different integration.

## Alternatives considered

| Candidate | Shape | Outcome |
| --- | --- | --- |
| own pool, leases in the store | serve DHCPv4, keep `leases` and `discoveries` | chosen |
| lease reading only | read an existing DHCP server's lease file | rejected, no schema to read portably and no control over allocation |
| MAC selector inside `internal/filter` | thread the MAC through the rule engine | rejected, identity stays in `internal/client`, and the filter keeps receiving resolved keys |
| resolve the lease on every query | read the store in the DNS path | rejected, a mutex-guarded map is enough and a lease table is small |
| hold the offer, or not | commit on offer and ack | chosen, it removes the double-offer race |
