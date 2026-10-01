// The dataset behind the README images.
//
// Every number and every name on a screenshot is produced by the real binary:
// configuration goes in over the real HTTP API, traffic comes in over real DNS
// from real source addresses, and the blocklists are the real published lists
// fetched over the real internet. Nothing here is written straight into the
// database except the one pass that spreads the query timestamps across the
// chart's day, which is a clock nobody has any other way to fake.

import { spawn } from "node:child_process";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export const LAN = "192.168.7.0/24";
export const SERVER = "192.168.7.1";

export const PROFILES = [
  { name: "kids", mode: "nxdomain" },
  { name: "work", mode: "null-address" },
  { name: "guest", mode: "refused" },
];

export const CLIENTS = [
  { name: "Studio MacBook", address: "192.168.7.34", profile: "work", notes: "primary workstation" },
  { name: "Kitchen iPad", address: "192.168.7.31", profile: "kids", notes: "kiosk by the toaster" },
  { name: "Pixel 8", address: "192.168.7.32", profile: "kids", notes: "" },
  { name: "Living Room TV", address: "192.168.7.33", profile: "default", notes: "Chromecast and the TV both" },
  { name: "Alex iPhone", address: "192.168.7.35", profile: "default", notes: "" },
  { name: "Office Printer", address: "192.168.7.36", profile: "default", notes: "Brother, only ever asks for time.*" },
  { name: "Guest Wi-Fi", address: "", prefix: "192.168.7.128/25", profile: "guest", notes: "the whole guest range" },
];

// Three real lists in the formats aegis parses. The Peter Lowe list was the
// obvious third choice and was dropped: it now answers with an HTML page
// wrapped around the list, which aegis correctly skips line by line and
// reports as zero rules.
export const SOURCES = [
  { name: "stevenblack", url: "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts", format: "hosts" },
  { name: "adguard", url: "https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt", format: "adblock" },
  { name: "cname-trackers", url: "https://raw.githubusercontent.com/AdguardTeam/cname-trackers/master/data/combined_disguised_trackers_justdomains.txt", format: "domains" },
];

export const UPSTREAMS = [
  { name: "cloudflare", url: "1.1.1.1:53" },
  { name: "quad9", url: "9.9.9.9:53", backup: true },
  { name: "google", url: "8.8.8.8:53", backup: true },
];

export const REWRITES = [
  { pattern: "nas.lan", target: "192.168.7.90" },
  { pattern: "printer.lan", target: "192.168.7.36" },
  { pattern: "*.lab.home", target: "192.168.7.5" },
];

export const SCHEDULES = [
  { name: "school nights", priority: 10, windows: [{ days: [0, 1, 2, 3, 4], start: "19:30", end: "21:30" }] },
  { name: "weekend gaming", priority: 20, windows: [{ days: [5, 6], start: "09:00", end: "22:00" }] },
];

export const RULES = [
  { domain: "ads.billing-portal.test", kind: "subdomains", action: "block", notes: "tracker on the invoice portal" },
  { domain: "cdn.jsdelivr.net", kind: "exact", action: "allow", notes: "the dashboard pulls its fonts from here" },
  { domain: "tiktokcdn.com", kind: "subdomains", action: "block", client: "Kitchen iPad" },
  { domain: "roblox.com", kind: "subdomains", action: "block", schedule: "school nights" },
  { domain: "meet.example.test", kind: "subdomains", action: "block", client: "Guest Wi-Fi" },
];

// The catalog ids a profile blocks by name. Anything missing from the catalog
// is skipped rather than sent, because the API takes ids, not labels.
//
// The kids set spans several groups on purpose: the catalog's first group is
// artificial intelligence, so a set with nothing in it photographs as a wall of
// empty checkboxes and shows nothing about what per-profile blocking does.
const KID_SERVICES = [
  "chatgpt",
  "gemini",
  "youtube",
  "netflix",
  "instagram",
  "tiktok",
  "snapchat",
  "roblox",
  "discord",
  "twitch",
];
const WORK_SERVICES = ["instagram", "tiktok"];
const SAFESEARCH = ["google", "bing", "duckduckgo", "youtube"];

export async function call(base, method, path, body) {
  const response = await fetch(`${base}${path}`, {
    method,
    headers: { "Content-Type": "application/json" },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    throw new Error(`${method} ${path} answered ${response.status}: ${await response.text()}`);
  }
  return response.status === 204 ? undefined : response.json();
}

// seedConfig puts a whole homelab's worth of policy into a fresh database
// through the same endpoints the web app calls.
export async function seedConfig(base, log = () => {}) {
  for (const source of SOURCES) {
    await call(base, "PUT", `/api/v1/sources/${source.name}`, { url: source.url, format: source.format, enabled: true, refresh_seconds: 21600 });
  }
  for (const profile of PROFILES) {
    await call(base, "PUT", `/api/v1/profiles/${profile.name}`, { mode: profile.mode });
  }
  await call(base, "PUT", "/api/v1/default-profile", { profile: "default" });
  for (const client of CLIENTS) {
    await call(base, "PUT", `/api/v1/clients/${client.name}`, {
      profile: client.profile,
      notes: client.notes,
      addresses: client.address ? [client.address] : [],
      macs: [],
      prefixes: client.prefix ? [client.prefix] : [],
    });
  }
  for (const upstream of UPSTREAMS) {
    await call(base, "PUT", `/api/v1/upstreams/${upstream.name}`, { url: upstream.url, enabled: true, backup: Boolean(upstream.backup) });
  }
  // The boot flag seeds its own peer named after the address, which would show
  // up as a fourth row duplicating quad9. The stored URL carries a udp:// scheme
  // the flag never had, so match on the address inside it.
  const seeded = await call(base, "GET", "/api/v1/upstreams");
  for (const row of seeded) {
    if (row.name !== "quad9" && row.url.endsWith("9.9.9.9:53")) {
      await call(base, "DELETE", `/api/v1/upstreams/${encodeURIComponent(row.name)}`);
    }
  }
  for (const rewrite of REWRITES) {
    await call(base, "PUT", `/api/v1/rewrites/${rewrite.pattern}`, { target: rewrite.target });
  }
  for (const schedule of SCHEDULES) {
    await call(base, "PUT", `/api/v1/schedules/${schedule.name}`, { priority: schedule.priority, windows: schedule.windows });
  }
  for (const rule of RULES) {
    const body = { domain: rule.domain, kind: rule.kind, action: rule.action };
    if (rule.notes) body.notes = rule.notes;
    if (rule.client) body.client = rule.client;
    if (rule.schedule) body.schedule = rule.schedule;
    await call(base, "POST", "/api/v1/rules", body);
  }

  const catalog = await call(base, "GET", "/api/v1/services");
  const known = new Set(catalog.services.map((service) => service.id));
  const pick = (wanted) => wanted.filter((id) => known.has(id));
  const kidBlocked = pick(KID_SERVICES);
  if (kidBlocked.length < KID_SERVICES.length) {
    log(`catalog is missing services: ${KID_SERVICES.filter((id) => !known.has(id)).join(", ")}`);
  }
  await call(base, "PUT", "/api/v1/profiles/kids/services", { services: kidBlocked });
  await call(base, "PUT", "/api/v1/profiles/work/services", { services: pick(WORK_SERVICES) });
  const engines = await call(base, "GET", "/api/v1/safesearch");
  const engineIDs = new Set(engines.engines.map((engine) => engine.id));
  await call(base, "PUT", "/api/v1/profiles/kids/safesearch", { engines: SAFESEARCH.filter((id) => engineIDs.has(id)) });
  await call(base, "PUT", "/api/v1/windows/game time", {
    action: "allow",
    schedule: "weekend gaming",
    clients: ["Pixel 8"],
    services: pick(["roblox", "youtube"]),
  });
  // Routing by suffix, so every configured peer actually carries traffic and the
  // latency column is measured rather than "unmeasured".
  // Each peer carries its own suffix, and every name under those suffixes is
// answered by an upstream rather than a rewrite, so all three latencies get
// measured. A peer that only ever saw rewritten names would read "unmeasured".
for (const route of [
    { domain: "corp.test", client: "", upstream: "quad9" },
    { domain: "example.test", client: "", upstream: "google" },
    { domain: "", client: "", upstream: "cloudflare" },
  ]) {
    await call(base, "POST", "/api/v1/routes", route);
  }
  await call(base, "PUT", "/api/v1/access", { allowed: ["192.168.7.0/24"], disallowed: [] });
  await call(base, "PUT", "/api/v1/stats/config", { interval: "168h", ignored: [] });
  log("configuration seeded");
}

// fetchSources pulls every list over the real internet. The rule counts in the
// Sources screenshot are whatever the published lists actually contain today.
export async function fetchSources(base, log = () => {}) {
  for (const source of SOURCES) {
    await call(base, "POST", `/api/v1/sources/${source.name}/refresh`);
    log(`fetched ${source.name}`);
  }
  const rows = await call(base, "GET", "/api/v1/sources");
  for (const row of rows) {
    if (row.last_error) {
      log(`source ${row.name} failed: ${row.last_error}`);
    }
  }
  return rows;
}

// The traffic mix. Names come in two weights: the trackers and ad hosts the
// real blocklists carry, and the ordinary names a household asks for. Each
// client has its own share, so the top-clients panel has a shape.
const BLOCKED = [
  ["doubleclick.net", 90],
  ["googleadservices.com", 80],
  ["googlesyndication.com", 75],
  ["pagead2.googlesyndication.com", 55],
  ["amazon-adsystem.com", 45],
  ["scorecardresearch.com", 40],
  ["adnxs.com", 35],
  ["criteo.com", 30],
  ["rubiconproject.com", 28],
  ["taboola.com", 22],
  ["outbrain.com", 18],
  ["adservice.google.com", 20],
  ["ads.yahoo.com", 16],
  ["ads.reddit.com", 14],
  ["ads.tiktokcdn.com", 12],
];

const ALLOWED = [
  ["www.google.com", 70],
  ["github.com", 55],
  ["api.github.com", 30],
  ["en.wikipedia.org", 40],
  ["news.ycombinator.com", 35],
  ["arxiv.org", 12],
  ["registry.npmjs.org", 20],
  ["cdn.jsdelivr.net", 18],
  ["fonts.gstatic.com", 14],
  ["deb.debian.org", 10],
  ["www.bbc.co.uk", 22],
  ["www.nytimes.com", 12],
  ["mail.google.com", 30],
  ["docs.google.com", 24],
  ["calendar.google.com", 10],
  ["www.apple.com", 10],
  ["icloud.com", 8],
  ["api.weather.com", 8],
  ["time.google.com", 14],
  ["music.apple.com", 8],
  ["www.spotify.com", 10],
  ["grafana.example.test", 14],
  ["prometheus.example.test", 10],
];

// Names under .lab.home are answered by the rewrite table and never reach a
// resolver; the .corp.test names are the ones the quad9 route sends upstream.
const LOCAL = [
  ["nas.lan", 22],
  ["printer.lan", 10],
  ["build.lab.home", 12],
  ["nas.lab.home", 10],
  ["wiki.lab.home", 9],
];

const ROUTED = [
  ["git.corp.test", 16],
  ["jira.corp.test", 12],
  ["wiki.corp.test", 10],
  ["grafana.corp.test", 9],
];

const TYPES = [
  ["A", 70],
  ["AAAA", 18],
  ["HTTPS", 8],
  ["TXT", 4],
];

function weighted(pairs) {
  const total = pairs.reduce((sum, [, weight]) => sum + weight, 0);
  let roll = Math.random() * total;
  for (const [value, weight] of pairs) {
    roll -= weight;
    if (roll <= 0) {
      return value;
    }
  }
  return pairs[pairs.length - 1][0];
}

// The household's shares, heaviest on the TV because that is what a real
// network looks like when the blocklists are on.
const TRAFFIC_CLIENTS = [
  ["192.168.7.33", 34],
  ["192.168.7.34", 26],
  ["192.168.7.31", 16],
  ["192.168.7.35", 14],
  ["192.168.7.32", 8],
  ["192.168.7.36", 2],
];

export function planTraffic(count) {
  const plan = [];
  for (let index = 0; index < count; index += 1) {
    const roll = Math.random();
    const name =
      roll < 0.4
        ? weighted(BLOCKED)
        : roll < 0.78
          ? weighted(ALLOWED)
          : roll < 0.93
            ? weighted(ROUTED)
            : weighted(LOCAL);
    plan.push({ name, type: weighted(TYPES), address: weighted(TRAFFIC_CLIENTS) });
  }
  return plan;
}

function dig(args) {
  return new Promise((resolve) => {
    const child = spawn("dig", args, { stdio: ["ignore", "ignore", "pipe"] });
    let errors = "";
    child.stderr.on("data", (chunk) => {
      errors += String(chunk);
    });
    child.on("close", (code) => resolve(code === 0 ? null : errors.trim()));
  });
}

// ask sends real DNS queries from a bound source address, in parallel. dig is
// the tool the README tells operators to use, so the capture uses it too.
export async function ask(plan, { port, host = SERVER, concurrency = 40, progress = () => {} } = {}) {
  let index = 0;
  let failures = 0;
  const workers = Array.from({ length: concurrency }, async () => {
    while (index < plan.length) {
      const item = plan[index];
      index += 1;
      const failure = await dig(["+time=2", "+tries=1", "-b", item.address, `@${host}`, "-p", String(port), item.name, item.type]);
      if (failure) {
        failures += 1;
      }
      if (index % 100 === 0) {
        progress(index, plan.length);
      }
    }
  });
  await Promise.all(workers);
  return failures;
}

// spreadTimestamps rewrites the query clock so the Overview's 24 hour chart has
// a day in it. Traffic that really happened in the last minute cannot fill 60
// buckets, and the clock is the one thing a capture cannot wait for. Nothing
// else about the rows changes: name, client, verdict, and rule stay exactly
// what the resolver wrote.
export function spreadTimestamps(dbPath, { hours = 24, newest = 90, stopMinutesAgo = 0 } = {}) {
  const script = `
import bisect, math, sqlite3, sys, time
path, hours, newest, stop_minutes = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
db = sqlite3.connect(path)
now = int(time.time() * 1000)
# The spread covers the day back to stop_minutes before now, and the caller fills
# that newest stretch with real queries: a chart's last bucket is a partial one
# either way, and this way the partial is real traffic rather than a gap.
start = now - hours * 3600000
stop = now - stop_minutes * 60000
span = stop - start
ids = [row[0] for row in db.execute("SELECT id FROM queries ORDER BY id DESC")]
# ids came back newest first, and the newest row must hold the latest moment or
# the log reads back to front. Reversed here, so the walk below runs oldest to
# newest in id order too.
older = ids[newest:][::-1]
def weight_at(moment):
    hour = (moment / 3600000.0) % 24
    # The floor is the overnight lull, not zero: a household still asks for the
    # time and its mail server, and a curve that reaches zero opens the chart
    # with a flat stretch that reads as missing data rather than as a quiet hour.
    return 0.55 + math.exp(-((hour - 20.5) ** 2) / 3.0) + 0.75 * math.exp(-((hour - 14.0) ** 2) / 6.0)
# The curve is sampled on a one-minute grid and inverted by interpolation. A
# coarse grid quantises the answer: with one slot per six minutes, consecutive
# rows land in the same minute of the same slot and the log grows a comb of
# spikes with empty minutes between them.
steps = max(int(span / 60000), 2)
curve = [weight_at(start + span * (index / steps)) for index in range(steps + 1)]
prefix = [0.0]
for value in curve:
    prefix.append(prefix[-1] + value)
total = prefix[-1]
updates = []
for index, row_id in enumerate(older):
    # Walk the cumulative curve from oldest to newest, one draw per row, so the
    # diurnal shape survives instead of every row landing in the same minute.
    target = total * ((index + 1) / len(older))
    slot = bisect.bisect_left(prefix, target)
    if slot >= steps:
        moment = stop
    else:
        # Clamped, because the last draw targets the end of the curve and an
        # unclamped fraction lands a few rows past it.
        fraction = min(max((target - prefix[slot]) / max(prefix[slot + 1] - prefix[slot], 1e-9), 0.0), 1.0)
        moment = start + span * (slot + fraction) / steps
    updates.append((int(moment), row_id))
db.executemany("UPDATE queries SET time = ? WHERE id = ?", updates)
db.commit()
print(db.execute("SELECT COUNT(*) FROM queries").fetchone()[0])
`;
  return new Promise((resolve, reject) => {
    const child = spawn("python3", ["-c", script, dbPath, String(hours), String(newest), String(stopMinutesAgo)], { stdio: ["ignore", "pipe", "pipe"] });
    let out = "";
    let err = "";
    child.stdout.on("data", (chunk) => {
      out += String(chunk);
    });
    child.stderr.on("data", (chunk) => {
      err += String(chunk);
    });
    child.on("close", (code) => (code === 0 ? resolve(Number(out.trim())) : reject(new Error(`spread timestamps failed: ${err.trim()}`))));
  });
}

// startServer boots the real binary and waits for the API to answer. The log
// lands next to the caller's console so a failed capture leaves the reason.
export async function startServer({ binary, db, api, dns, extraArgs = [], log = () => {} }) {
  const args = ["serve", "--db", db, "--api-address", api, "--dns-address", dns, "--log-level", "warn", ...extraArgs];
  const child = spawn(binary, args, { stdio: ["ignore", "ignore", "pipe"] });
  let noise = "";
  child.stderr.on("data", (chunk) => {
    noise += String(chunk);
  });
  const base = `http://${api}`;
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${base}/api/v1/status`);
      if (response.ok) {
        log(`aegis serving dns ${dns}, api ${api}`);
        return { child, base, stop: () => child.kill("SIGKILL"), noise: () => noise };
      }
    } catch {
      // Not listening yet.
    }
    await sleep(100);
  }
  child.kill("SIGKILL");
  throw new Error(`aegis never came up on ${api}: ${noise}`);
}