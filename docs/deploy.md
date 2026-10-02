# Deploys

Aegis ships as one static binary and one multi-arch container, and nothing else
lives between the build and a running resolver. There is no configuration
management and no orchestrator; upgrades are one command against a published
release. This records the two supported ways to run it, how a release is cut,
and where the boundary sits.

## The decision

An operator either copies the binary and runs it under systemd, or runs the
published image. Both read the same SQLite file and the same flags, so moving
between them is copying that file. Installing the system copy takes one
sudo; running it never does, because DNS sits on a high port while a resolver
already holds port 53, which is the common case on a home network.

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

## Raspberry Pi: binary, a system unit, one sudo

The install takes root once and the service never gets it back. The binary
lands in `/usr/local/bin`, the database in `/var/lib/aegis`, and systemd runs
the unit as a dedicated `aegis` user that can write only the data directory.
The version in the paths below is the release being installed; every asset is
named `aegis_<version>_<os>_<arch>` and `checksums.txt` beside it covers it.

    curl -sL https://github.com/McCune1224/aegis/releases/download/v0.2.0/aegis_0.2.0_linux_arm64.tar.gz \
      | tar -C /tmp -xz aegis
    sudo install -m 0755 /tmp/aegis /usr/local/bin/aegis
    sudo useradd --system --home-dir /var/lib/aegis --shell /usr/sbin/nologin aegis
    sudo install -d -o aegis -g aegis -m 0755 /var/lib/aegis

    /etc/systemd/system/aegis.service:

    [Unit]
    Description=Aegis DNS sinkhole
    After=network-online.target
    Wants=network-online.target

    [Service]
    Type=simple
    User=aegis
    Group=aegis
    ExecStart=/usr/local/bin/aegis serve --db /var/lib/aegis/aegis.db \
      --dns-address 0.0.0.0:15353 --api-address 127.0.0.1:18099 \
      --upstream 9.9.9.9:53
    Restart=on-failure
    RestartSec=3

    [Install]
    WantedBy=multi-user.target

    sudo systemctl daemon-reload
    sudo systemctl enable --now aegis

The high port is deliberate. AdGuard Home already holds port 53 and keeps it.
The cutover this document used to describe was retired on 2026-09-24. Aegis
stays a second resolver on `:15353` and never takes `:53`, so there is no
cutover to survive and no household rollback to write.

### Without sudo

Where sudo is unavailable the same unit runs as a user unit in the home
directory, at the cost of one install only this user owns. Write it to
`~/.config/systemd/user/aegis.service`, drop `User=` and `Group=`, set
`WantedBy=default.target`, and point `ExecStart` at `%h/aegis` with the
database at `%h/aegis-data/aegis.db`:

    scp aegis_0.2.0_linux_arm64.tar.gz pi:~/aegis.tar.gz
    ssh pi 'tar -C ~ -xzf aegis.tar.gz && mkdir -p ~/aegis-data ~/.config/systemd/user'

    loginctl enable-linger "$USER"
    systemctl --user daemon-reload
    systemctl --user enable --now aegis

`enable-linger` is what makes a user unit outlive the SSH session and start at
boot; without it the service stops when the last session closes.

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

## Updating

`aegis update` replaces the running binary with the newest published release:

    sudo aegis update

It reads the release metadata, downloads the asset for this platform, checks
it against the release's `checksums.txt`, moves the new binary over the old
one, and restarts the service when systemd is already running it. A build
that carries no release version, or one past the newest tag, is refused
unless `--force` says otherwise, so a snapshot is never silently replaced.

    aegis update --check             # report only, safe to run from cron
    aegis update --repo fork/name    # another repository's releases

A box running the user unit above runs the same command as that user, with
no sudo. Updating a container is not this command's job; pull the image
instead.

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

**An always-on updater.** `aegis update` is the update, run by the operator
or by one cron line. A process that polls for releases and rewrites the
resolver on its own adds a second process with the power to replace the
resolver, and it inherits the job of deciding when a household is ready for
a restart. Checking from the running service so the console can badge an
available version is the same decision and stays deferred with it. Revisit if
enough hosts make the command the bottleneck.

**A shipped unit file.** The unit above lives in this document rather than in
the repo. Its paths are now the same on every host, but the port, the
upstream, and the listen addresses are the operator's choices, and a
templated unit would carry them as defaults. Revisit when it ships as part of
an `aegis service install` command that writes the unit from the values
passed at install time.
