// Regenerates every image in the README and docs/.
//
// One run: boot the real binary against a throwaway database, seed it through
// the real API, fetch the real blocklists over the real internet, ask real
// DNS questions from real client addresses, spread the query clock across a
// day, then photograph the console in a real browser over CDP.
//
// The stub upstream means the three public resolvers aegis is pointed at are
// loopback aliases inside the capture namespace, so the boot has no route off
// the box but every verdict, rule match, and log row is the product's own.
//
//   node scripts/media/shoot.mjs [--keep] [--only=overview,clients]
//
// --keep leaves the server up on 127.0.0.1:18090 for a look by hand.

import { Browser } from "./cdp.mjs";
import { SERVER, ask, fetchSources, planTraffic, seedConfig, spreadTimestamps, startServer } from "./seed.mjs";
import { mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, spawnSync } from "node:child_process";

const ROOT = join(dirname(fileURLToPath(import.meta.url)), "../..");
const WORK = join(ROOT, ".media");
const OUT = join(ROOT, "docs/media");
// High, out of the way of the dev stack's own ports, and overridable so two
// captures can run without colliding.
const API_PORT = Number(process.env.AEGIS_MEDIA_API ?? 18120);
const DNS_PORT = Number(process.env.AEGIS_MEDIA_DNS ?? 15453);
const QUERIES = Number(process.env.AEGIS_MEDIA_QUERIES ?? 2400);
// The Overview's last chart bucket is a quarter of an hour wide. One query every
// six seconds puts about 150 rows in it across the capture, which is the rate
// the spread puts in the neighbouring buckets, so the line runs off the right
// edge instead of spiking at it.
const TRICKLE_MS = Number(process.env.AEGIS_MEDIA_TRICKLE_MS ?? 6000);

// --keep leaves the servers and the seeded database in place after the run, so
// the console can be opened by hand at 127.0.0.1:<api port>.
const keep = process.argv.includes("--keep");
const onlyArg = process.argv.find((argument) => argument.startsWith("--only="));
const only = onlyArg ? onlyArg.slice("--only=".length).split(",") : null;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const log = (message) => process.stdout.write(`media  ${message}\n`);
const wanted = (name) => !only || only.includes(name);

// Every image is a photograph of the real console in a real viewport, at the
// console's own breakpoints: 1440 for the workbench, 820 for the tablet
// layout, 400 for the phone. Heights are chosen per view so a long table shows
// rows rather than being cut at a scroll line; the console scrolls inside the
// frame, so a taller viewport means more rows, never a stretched page.
const VIEWS = [
  {
    name: "overview",
    hash: "#/",
    width: 1440,
    height: 960,
    ready: "document.querySelector('[data-testid=stat-total]')?.textContent !== '0'",
    // The chart defaults to the last hour, where every seeded row landed, so it
    // draws one spike. 24h is the view that shows a day of a network's traffic,
    // and the readiness check is the active segment rather than the chart, which
    // is present either way.
    click: { selector: ".seg-btn", text: "24h" },
    clicked: ".seg-btn.active",
  },
  {
    name: "query-log",
    hash: "#/log",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid=log-row]').length > 20",
  },
  {
    name: "constellation",
    hash: "#/constellation",
    width: 1440,
    height: 900,
    ready: "document.querySelector('[data-testid=graph]')",
    settle: 2200,
  },
  {
    name: "clients",
    hash: "#/clients",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid=client-row]').length >= 6",
    // Selecting a row opens the drawer with that client's real addresses, so
    // the shot shows the editor rather than the empty inspector. The Studio
    // MacBook is the one with notes and a non-default profile.
    click: { selector: "[data-testid=client-row]", text: "Studio MacBook" },
    clicked: "[data-testid=client-profile]",
  },
  {
    name: "profiles",
    hash: "#/profiles",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid=profile-row]').length >= 3",
    // The kids profile is the one with a safe-search set and a mode of its own,
    // so the drawer shows a policy rather than four empty fields.
    click: { selector: "[data-testid=profile-row]", text: "kids" },
    clicked: "[data-testid=profile-mode]",
  },
  {
    name: "rules",
    hash: "#/rules",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid=rules-new]').length >= 0 && document.querySelectorAll('tbody tr').length >= 5",
  },
  {
    name: "schedules",
    hash: "#/services",
    width: 1440,
    height: 900,
    // Schedules and service windows are one view now: the editor lives at the
    // foot of Blocked Services, so a short catalog brings the window rows onto
    // the screen on their own.
    ready: "document.querySelectorAll('[data-testid^=service-]').length > 40",
    // The catalog is a long scroll above the window editor. A search matching
    // one service leaves the catalog short and brings the windows and schedules
    // onto the screen, without a scroll position baked into the shot.
    type: { selector: "[data-testid=services-search]", text: "youtube" },
    clicked: "[data-testid=window-row]",
    settle: 900,
  },
  {
    name: "blocked-services",
    hash: "#/services",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid^=service-]').length > 40",
    // The default profile blocks no services, which photographs as a wall of
    // empty checkboxes. The kids profile is the one with a real set on it, and
    // the search box narrows the catalog to the groups that set covers.
    choose: { selector: "[data-testid=services-scope]", option: "kids" },
    // No search: the catalog opens on its first group, artificial intelligence,
    // and the kids profile blocks two of those, so the checked boxes are the
    // first thing on screen rather than a scroll away.
    clicked: "[data-testid=service-chatgpt]",
  },
  {
    name: "sources",
    hash: "#/system/sources",
    width: 1440,
    height: 900,
    ready: "document.querySelectorAll('[data-testid=source-health]').length >= 3",
    online: true,
  },
  {
    name: "upstreams",
    hash: "#/system/upstreams",
    width: 1440,
    height: 900,
    // The list rows carry no name testid, so the health badge is the honest
    // count: one per configured peer, and the seed names three.
    ready: "document.querySelectorAll('[data-testid=upstream-health]').length >= 3",
  },
  {
    name: "overview-tablet",
    hash: "#/",
    width: 820,
    height: 1180,
    ready: "document.querySelector('[data-testid=stat-total]')?.textContent !== '0'",
    click: { selector: ".seg-btn", text: "24h" },
    clicked: "[data-testid=chart]",
  },
  {
    name: "query-log-phone",
    hash: "#/log",
    width: 400,
    height: 860,
    ready: "document.querySelectorAll('[data-testid=log-row]').length > 20",
    // Seven columns cannot fit 400 CSS px, and the table scrolls sideways inside
    // its own frame rather than the page. The shot is the scroll container's own
    // width, so the columns a phone actually shows are the ones in the picture.
    shotWidth: "document.querySelector('.tbl-wrap')?.scrollWidth ?? 400",
    settle: 800,
  },
];

// The Sources view is photographed here, not in the namespace. The capture LAN
// has no route off the box, so every boot-time refresh in there fails and the
// panel would show three red rows that no real deployment has. Here the lists
// are fetched over the real internet, so the rule counts are the published
// counts and every badge reads ok.
const ONLINE_VIEWS = VIEWS.filter((view) => view.online);

// configure boots aegis outside the network namespace, because fetching the
// blocklists needs the real internet. The database is the hand-off: the second
// boot, inside the namespace, reads every setting from it, which is the
// behaviour the boot-once contract already guarantees.
async function configure() {
  rmSync(WORK, { recursive: true, force: true });
  mkdirSync(WORK, { recursive: true });
  const db = join(WORK, "media.db");
  const first = await startServer({
    binary: join(ROOT, "bin/aegis"),
    db,
    api: `127.0.0.1:${API_PORT}`,
    dns: `127.0.0.1:${DNS_PORT + 100}`,
    log,
  });
  const problems = [];
  try {
    await seedConfig(first.base, log);
    await fetchSources(first.base, log);
    problems.push(...(await capture(first.base, ONLINE_VIEWS)));
  } finally {
    first.stop();
    await sleep(400);
  }
  return problems;
}

// Phase two runs inside a rootless network namespace holding a throwaway
// 192.168.7.0/24, so every client has a real address the resolver can name and
// the query log reads like a household's network rather than a loopback test
// rig. The three public resolvers become loopback aliases for the stub.
async function seed() {
  const db = join(WORK, "media.db");
  log("phase 2: starting the stub upstream and aegis on the LAN address");
  const stub = spawn("node", [join(ROOT, "scripts/media/upstream.mjs")], {
    stdio: ["ignore", "pipe", "inherit"],
    // Port 53, on the loopback aliases the three configured resolvers got. The
    // namespace is rootless but its own root, so the privileged port binds.
    env: { ...process.env, STUB_DNS_PORT: "53", STUB_DNS_DELAY: "9" },
  });
  await sleep(500);
  const server = await startServer({
    binary: join(ROOT, "bin/aegis"),
    db,
    api: `127.0.0.1:${API_PORT}`,
    dns: `${SERVER}:${DNS_PORT}`,
    log,
  });

  log(`phase 3: asking ${QUERIES} real DNS questions from six client addresses`);
  const plan = planTraffic(QUERIES);
  let lastReported = 0;
  const failures = await ask(plan, {
    port: DNS_PORT,
    concurrency: 48,
    progress: (done, total) => {
      if (done - lastReported >= 400) {
        lastReported = done;
        log(`  ${done}/${total}`);
      }
    },
  });
  log(`  ${QUERIES - failures} answered, ${failures} dig failures`);

  await sleep(2500);
  // The spread runs right up to now, so the chart's last bucket carries the same
  // rate as the rest of the day. Anything that stopped short of now leaves a
  // flat stretch there, and a flat stretch at the right-hand edge reads as a
  // gap rather than as a partial bucket.
  const rows = await spreadTimestamps(db, { hours: 24, newest: 4 });
  log(`phase 4: query log holds ${rows} rows, spread across the day`);

  return { db, server, stub };
}

// trickle keeps a little traffic on the wire while the camera is open, so the
// Constellation's pulses and the Overview's live feed are moving in the shot.
// It runs at roughly the rate the spread implies for the current hour, so the
// rows it adds land in the newest bucket as a rate and not as a spike.
function trickle() {
  const names = ["www.google.com", "doubleclick.net", "github.com", "nas.lan", "googleadservices.com"];
  let index = 0;
  const timer = setInterval(() => {
    const address = ["192.168.7.33", "192.168.7.34", "192.168.7.31", "192.168.7.35"][index % 4];
    const name = names[index % names.length];
    index += 1;
    const child = spawn("dig", ["+time=2", "+tries=1", "-b", address, `@${SERVER}`, "-p", String(DNS_PORT), name, "A"], { stdio: "ignore" });
    child.on("close", () => {});
  }, TRICKLE_MS);
  return () => clearInterval(timer);
}

// capture photographs each view of a running console and returns what the run
// should complain about: a view that never rendered, a console that logged an
// error, or a layout that overflowed its own viewport.
async function capture(base, views) {
  const wantedViews = views.filter((view) => wanted(view.name));
  if (wantedViews.length === 0) {
    return [];
  }
  const browser = await Browser.launch({ port: 9400 });
  const page = await browser.newPage();
  const problems = [];
  try {
    for (const view of wantedViews) {
      await page.viewport({ width: view.width, height: view.height, scale: 2, mobile: view.width < 500 });
      // A fresh document per view, keyed on the view's own name. A hash-only
      // change is a same-document navigation: no load event fires and the
      // previous view's fetches are still in flight, so the shot would race
      // the render. The key makes every navigation cross a document boundary.
      await page.open(`${base}/?shot=${view.name}${view.hash}`);
      await page.waitFor("document.fonts.status === 'loaded'", { timeoutMs: 5000 }).catch(() => {});
      try {
        await page.waitFor(view.ready, { label: `${view.name} rendered` });
      } catch (cause) {
        await page.shot(join(WORK, `failed-${view.name}.png`));
        problems.push(`${view.name}: ${cause.message}; console ${[...new Set(page.problems)].join(" | ") || "clean"}`);
        page.problems.length = 0;
        log(`  FAILED ${view.name}, wrote ${join(WORK, `failed-${view.name}.png`)}`);
        continue;
      }
      // A click goes through Input.dispatchMouseEvent, not element.click():
      // Solid delegates its handlers, and a synthetic click never reaches them.
      // The pointer stays where it clicked, so a shot taken after a click shows
      // the hover state, which is what an operator's own cursor would show.
      if (view.click) {
        await page.click(view.click.selector, { text: view.click.text });
      }
      if (view.choose) {
        await page.choose(view.choose.selector, view.choose.option);
      }
      if (view.type) {
        await page.type(view.type.selector, view.type.text);
      }
      if (view.clicked) {
        await page.waitFor(`document.querySelector('${view.clicked}')`, { label: `${view.name} ready after interaction` });
      }
      await sleep(view.settle ?? 700);
      mkdirSync(OUT, { recursive: true });
      // A view may ask for a wider frame than its viewport, which is how a table
      // that scrolls sideways gets photographed with the columns a phone shows
      // rather than the ones it hides past the edge.
      if (view.shotWidth) {
        await page.viewport({ width: Math.ceil(await page.evaluate(view.shotWidth)), height: view.height, scale: 2, mobile: true });
        await sleep(400);
      }
      await page.shot(join(OUT, `${view.name}.png`));
      if (view.shotWidth) {
        await page.viewport({ width: view.width, height: view.height, scale: 2, mobile: true });
      }
      const box = await page.evaluate("({ width: document.documentElement.scrollWidth, view: window.innerWidth })");
      if (box.width > box.view + 1) {
        problems.push(`${view.name}: horizontal overflow ${box.width}px in a ${box.view}px viewport`);
      }
      if (page.problems.length) {
        problems.push(`${view.name}: ${[...new Set(page.problems)].join(" | ")}`);
        page.problems.length = 0;
      }
      log(`shot ${view.name} (${view.width}px)`);
    }
  } finally {
    await browser.close();
  }
  return problems;
}

async function onTheLAN() {
  const { server, stub } = await seed();
  const problems = [];
  try {
    // The trickle runs for the length of the capture, so the Constellation's
    // pulses and the Overview's live feed are moving in the shot.
    const stopTrickle = trickle();
    try {
      problems.push(...(await capture(`http://127.0.0.1:${API_PORT}`, VIEWS.filter((view) => !view.online))));
    } finally {
      stopTrickle();
    }
  } finally {
    server.stop();
    stub.kill("SIGKILL");
    if (keep) {
      log(`kept aegis on 127.0.0.1:${API_PORT} and its database in ${WORK}`);
    } else {
      rmSync(WORK, { recursive: true, force: true });
    }
  }
  return problems;
}

function inNamespace() {
  // A throwaway 192.168.7.0/24 on loopback, plus loopback aliases for the
  // three public resolvers the stub answers for.
  const script = `ip link set lo up
ip addr add ${SERVER}/24 dev lo
for alias in 1.1.1.1 9.9.9.9 8.8.8.8; do ip addr add $alias/32 dev lo; done
exec node scripts/media/shoot.mjs ${process.argv.slice(2).join(" ")}`;
  const inner = spawnSync("unshare", ["--map-root-user", "--net", "sh", "-c", script], {
    cwd: ROOT,
    stdio: "inherit",
    env: { ...process.env, AEGIS_MEDIA_IN_NAMESPACE: "1" },
  });
  process.exitCode = inner.status ?? 1;
}

async function shoot() {
  if (process.env.AEGIS_MEDIA_IN_NAMESPACE) {
    report(await onTheLAN());
    return;
  }
  report(await configure());
  // The LAN pass runs in a child namespace and reports through its exit code.
  inNamespace();
}

function report(problems) {
  if (problems.length === 0) {
    return;
  }
  log("problems:");
  for (const problem of problems) {
    log(`  ${problem}`);
  }
  process.exitCode = 1;
}

await shoot();