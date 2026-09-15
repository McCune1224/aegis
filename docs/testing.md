# How to test aegis

Five tiers. A change needs evidence from the tier its behavior lives in, and most
work stops at the second one. Only the last tier needs different hardware.

## Tier 1. Pure tests

No sockets, no files. Runs in milliseconds.

```
go test ./internal/filter ./internal/blocklist
```

This covers rule matching, list parsing, and the atomic rule-set swap. If a
change touches only these, this tier is the whole check.

## Tier 2. Local sockets

Real UDP and TCP on `127.0.0.1` with port `0`, so the kernel picks the port and
tests never collide. A stub DNS server and the aegis server both run in process.

```
go test ./internal/dns
```

No privileges and no network access, so this runs in CI. This tier is where the
server, the forwarder, truncation handling, and shutdown get checked. Prefer it
over a mock, because the wire bytes are what a real client sees.

## Tier 3. A virtual network with several clients

Per-client policy needs distinct source addresses. A rootless network namespace
gives you a throwaway one, with no sudo:

```
unshare -rn sh -c '
  ip link set lo up
  ip addr add 10.9.9.1/24 dev lo
  ip addr add 10.9.9.2/24 dev lo
  ip addr add 10.9.9.3/24 dev lo
  exec "$SHELL"
'
```

Inside that namespace, `10.9.9.1` is the server and `10.9.9.2` through `10.9.9.4`
stand in for separate devices. `dig -b 10.9.9.2 @10.9.9.1 -p 15353` selects which
one asks. The namespace disappears when the shell exits. Add more addresses for
more devices.

This is the check that proves per-client policy end to end. Run aegis with two
profiles and ask the same blocked name from two addresses:

```
./bin/aegis serve \
  --dns-address 10.9.9.1:15353 \
  --upstream 127.0.0.1:1 \
  --db /tmp/aegis.db \
  --blocklist ./list.txt \
  --profile kids=refused \
  --client 10.9.9.2=kids \
  --log-level error &

dig -b 10.9.9.2 @10.9.9.1 -p 15353 ads.example.com   # REFUSED, the kids profile
dig -b 10.9.9.3 @10.9.9.1 -p 15353 ads.example.com   # NXDOMAIN, the default
```

The upstream is unreachable on purpose. A blocked name never reaches it, so the
check needs no working resolver and no internet access, which is what makes it
runnable inside the namespace.

### Proving the database is the source of truth

The first boot seeds the database from the flags. Every boot after that ignores
them, which is what lets the web app take over without a flag overwriting it.
Run the server twice against one database, the second time with no profile or
client flags at all, and confirm the answers do not change.

```
./bin/aegis serve ... --db /tmp/aegis.db --profile kids=refused --client 10.9.9.2=kids
# log says: seeded an empty database from the flags

./bin/aegis serve ... --db /tmp/aegis.db
# no seeded line, and 10.9.9.2 still gets REFUSED
```

## Tier 4. Cross-architecture build

The build half of the multi-arch claim is checkable on any machine:

```
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /tmp/aegis-armv7 ./cmd/aegis
CGO_ENABLED=0 GOOS=linux GOARCH=arm64  go build -o /tmp/aegis-arm64 ./cmd/aegis
file /tmp/aegis-armv7 /tmp/aegis-arm64
```

GNU `file` reports `ELF 32-bit LSB executable, ARM, EABI5` and `ELF 64-bit LSB
executable, ARM aarch64`, both statically linked. Confirm that output rather than
trusting a green build, because a CGO dependency would silently produce a dynamic
binary that fails on a Pi with a different libc.

## Tier 5. Real ARM hardware

This is the only tier that needs the Pi, and only for two questions the other
tiers cannot answer. Does the binary run on 32-bit ARM at all, and what is the
filter cost on a Pi's CPU rather than on a development machine's.

Running an ARM binary locally is not an option here. No `binfmt_misc` entry is
registered, so the kernel does not know how to execute one. Registering it needs
root, which makes the Pi the simpler path.

### Using the Pi without disturbing the household

AdGuard Home holds port 53 there. Keep it, and run aegis on a high port beside
it:

```
scp /tmp/aegis-armv7 pi:~/aegis
ssh pi '~/aegis serve --dns-address 0.0.0.0:5353 --upstream 9.9.9.9:53'
dig -p 5353 @<pi-address> ads.example.com
```

The household keeps resolving through AdGuard the whole time. Only for the final
"does the television stop showing ads" check does aegis take port 53, and only
after it can manage upstreams and roll back. Do not remove AdGuard before then,
because aegis has no way to put the network back yet.

## What this means for a VM

Nothing in tiers 1 through 4 needs a VM. Tier 3 covers a virtual network. Tier 5
covers foreign hardware. A VM would only add value for a question neither covers,
such as testing the DHCP server against a real DHCP client, or testing on a
distribution other than the development machine. Both are later than phase one.

## Where the gaps live

The tracker owns which tiers are exercised and which are not. This document
records what each tier is for and how to run it, not its status.
