# Deploys

Aegis ships as one static binary and one multi-arch container, and nothing else
lives between the build and a running resolver. There is no configuration
management, no orchestrator, and no auto-updater. This records the two supported
ways to run it, how a release is cut, and where the boundary sits.

## The decision

An operator either copies the binary and runs it under systemd, or runs the
published image. Both read the same SQLite file and the same flags, so moving
between them is copying that file. Neither path needs root for DNS when a
resolver already holds port 53, which is the common case on a home network.

The alternative was a packaged artifact per distribution (deb, rpm, an installer
script). It was rejected because it multiplies the things that can drift from
the release matrix, and the binary already runs on a Pi, a NAS, a router, and a
laptop with no dependency beyond a libc-free static ELF.

## Cutting a release

A `v*` tag runs `.github/workflows/release.yml`, and a tag is the only thing
that publishes:

1. Build the web bundle. The binary embeds it with `go:embed`, so a release
   built without this step ships an empty UI.
2. `goreleaser release` builds amd64, arm64, armv7, and armv6 for linux, plus
   darwin and windows, writes the archives and `checksums.txt`, and creates the
   GitHub release.
3. `docker buildx` publishes the same linux arches to
   `ghcr.io/<owner>/aegis:<tag>` and `:latest`.

`ci.yml` runs on `main` and on pull requests. It builds a snapshot, asserts the
snapshot reports its version rather than `dev`, and asserts the snapshot serves
the app it embedded, so the embed step cannot silently rot again. It never
publishes.

A release is therefore:

    git tag v0.1.0
    git push origin v0.1.0

## Raspberry Pi: binary and a user systemd unit

No root is needed. The DNS listener uses a high port and the API stays on
loopback, so the whole service runs as the login user.

    scp aegis-linux-arm64 pi:~/aegis
    ssh pi 'chmod +x ~/aegis && mkdir -p ~/aegis-data ~/.config/systemd/user'

    ~/.config/systemd/user/aegis.service:

    [Unit]
    Description=Aegis DNS sinkhole
    After=network-online.target
    Wants=network-online.target

    [Service]
    Type=simple
    ExecStart=%h/aegis serve --db %h/aegis-data/aegis.db \
      --dns-address 0.0.0.0:15353 --api-address 127.0.0.1:18099 \
      --upstream 9.9.9.9:53
    Restart=on-failure
    RestartSec=3

    [Install]
    WantedBy=default.target

    loginctl enable-linger "$USER"
    systemctl --user daemon-reload
    systemctl --user enable --now aegis

`enable-linger` is what makes a user unit outlive the SSH session and start at
boot; without it the service stops when the last session closes.

The high port is deliberate. AdGuard Home already holds port 53 and keeps it.
The cutover this document used to describe was retired on 2026-09-24. Aegis
stays a second resolver on `:15353` and never takes `:53`, so there is no
cutover to survive and no household rollback to write.

## What the Pi blocks

The unit above starts with an empty database, so blocking is a decision made
through the API rather than a flag. Since 2026-09-24 the evaluation unit
enables two sources, `adguard-dns` and `oisd-basic`, both on a daily refresh,
and the `default` profile blocks the catalog's gambling and dating services
(betano, betfair, betway, blaze, fdj_united, grindr, plenty_of_fish, tinder,
wizz). Two ad-focused sources and the two service groups with the least
collateral were the small set chosen to make the unit enforce something real
while leaving streaming, social, messaging, and the AI services untouched.

Both settings live in the API, so rolling them back is three calls:

    ssh pi 'curl -sX DELETE http://127.0.0.1:18099/api/v1/sources/adguard-dns'
    ssh pi 'curl -sX DELETE http://127.0.0.1:18099/api/v1/sources/oisd-basic'
    ssh pi 'curl -sX PUT http://127.0.0.1:18099/api/v1/profiles/default/services \
      -H "Content-Type: application/json" --data "{\"services\":[]}"'

Verify against the high port. `dig -p 15353 @<pi-address> doubleclick.net`
answers NXDOMAIN while blocking is on and NOERROR after the rollback.

## Container

The image is `scratch` plus the binary, a CA bundle, and a volume for the
database, and it runs as an unprivileged user:

    docker run -d --name aegis \
      -p 53:53/udp -p 53:53/tcp -p 8080:8080 \
      -v aegis-data:/var/lib/aegis \
      ghcr.io/<owner>/aegis:latest

The default command serves `--dns-address 0.0.0.0:53 --api-address 0.0.0.0:8080`
with the database on the volume. Override the command to add upstreams, sources,
or a high DNS port beside an existing resolver.

## Deferred, with the reason

**Auto-update and rollback.** The service is a file and a unit; replacing the
file and restarting is the update. A watchtower-style puller or a self-updater
adds a second process with the power to replace the resolver, for a task an
operator does by hand once a release. Revisit if there are enough hosts that
manual updates become the bottleneck.

**A shipped unit file.** The unit above lives in this document rather than in
the repo, because the paths, the port, and the upstream are host-specific and a
templated unit would carry every operator's choices as defaults. Revisit when a
second host needs the same file and the differences are values rather than
structure.
