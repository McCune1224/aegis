// Drive the real console in a real Chromium over CDP, against the real binary
// on an ephemeral port with a throwaway SQLite store. This is the executable
// check for dogfooding behavior no unit test can see: request counts, write
// round trips, and a graph that renders to SVG.
//
//   node scripts/e2e/drive.mjs baseline     write round trips on the current build
//   node scripts/e2e/drive.mjs gaps         the three dogfooding gap proofs
//   node scripts/e2e/drive.mjs prove        save-then-block, then the full-wipe round trip
//
// The binary under test is AEGIS_BIN (default bin/aegis). Everything else is
// picked free at run time and thrown away at exit.

import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Browser } from "../media/cdp.mjs";

const REPO = new URL("../..", import.meta.url).pathname;
const BIN = process.env.AEGIS_BIN ?? join(REPO, "bin/aegis");

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function freePort() {
  const run = spawnSync("python3", ["-c", "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1])"], { timeout: 5000 });
  if (run.status !== 0) {
    throw new Error(`no free port: ${run.stderr}`);
  }
  return Number(run.stdout.toString().trim());
}

async function waitUntilHealthy(base, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${base}/api/v1/status`);
      if (response.ok) {
        return;
      }
    } catch {
      // not up yet
    }
    await sleep(120);
  }
  throw new Error(`aegis never answered on ${base}`);
}

// waitUntil polls one condition until it holds. Waiting on state rather than on
// a clock is what keeps a slow build from failing a proof that would pass.
async function waitUntil(fn, label, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      if (await fn()) {
        return;
      }
    } catch {
      // not readable yet
    }
    await sleep(120);
  }
  throw new Error(`never became true: ${label}`);
}

class Rig {
  constructor({ bin }) {
    this.bin = bin;
  }

  async up() {
    this.dir = mkdtempSync(join(tmpdir(), "aegis-e2e-"));
    this.blocklist = join(this.dir, "block.txt");
    writeFileSync(this.blocklist, "ads.example.com\ntracker.example.net\n");
    this.sourceServer = await this.serveSource();
    this.sourcePort = this.sourceServer.address().port;
    this.ports = { api: freePort(), dns: freePort(), devtools: freePort() };
    this.base = `http://127.0.0.1:${this.ports.api}`;

    this.aegis = spawn(this.bin, [
      "serve",
      "--db", join(this.dir, "aegis.db"),
      "--api-address", `127.0.0.1:${this.ports.api}`,
      "--dns-address", `127.0.0.1:${this.ports.dns}`,
      "--upstream", "127.0.0.1:1",
      "--profile", "kids=refused",
      "--client", "10.9.9.2=kids",
      "--blocklist", `hosts:${this.blocklist}`,
      "--source", `e2e-list=http://127.0.0.1:${this.sourcePort}/list`,
      "--log-level", "warn",
    ], { stdio: ["ignore", "pipe", "pipe"] });
    this.aegis.stderr.on("data", (chunk) => process.stderr.write(chunk));
    await waitUntilHealthy(this.base);

    await this.seed();
    return this;
  }

  // The source write path needs a fetchable URL, so the run serves one
  // blocklist from the harness itself.
  serveSource() {
    return import("node:http").then(({ createServer }) => {
      const server = createServer((request, response) => {
        response.setHeader("Content-Type", "text/plain");
        response.end("annoying-ads.example.com\npixel.example.net\n");
      });
      return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
    });
  }

  async seed() {
    const put = async (path, body) => {
      const response = await fetch(`${this.base}${path}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!response.ok) {
        throw new Error(`seed ${path}: HTTP ${response.status} ${await response.text()}`);
      }
    };
    await put("/api/v1/rewrites/tv.local", { target: "10.9.9.50" });
    // A household's worth of saved clients, so list pages and the observed
    // GROUP BY run at dogfooding weight.
    for (let i = 0; i < 30; i += 1) {
      await put(`/api/v1/clients/devices-${String(i + 1).padStart(2, "0")}`, {
        profile: "default",
        notes: "",
        addresses: [`10.9.9.${100 + i}`],
        macs: [],
        prefixes: [],
      });
    }

    // Queries from 127.0.0.1 are query-log observations no client claims, which
    // is one of the two passive-device sources the Constellation draws.
    for (const name of ["ads.example.com", "ads.example.com", "example.com", "tracker.example.net"]) {
      spawnSync("dig", ["+short", "-p", String(this.ports.dns), "@127.0.0.1", name], { timeout: 5000 });
    }

    // The log flushes once a second and the page reads its observed feed once
    // when it boots, so the rows these digs produced have to be readable before
    // the browser opens. Otherwise the graph is built from a feed that has never
    // heard of the address and the device never draws.
    await waitUntil(async () => {
      const log = await (await fetch(`${this.base}/api/v1/queries?client=127.0.0.1&limit=1`)).json();
      return log.queries.length > 0;
    }, "the seeded queries reach the query log");

    // Discoveries have no API write path; the DHCP server is the only writer.
    // A run seeds the rows directly, the same class of write the media capture
    // uses for its timestamp spread.
    const script = `
      const { DatabaseSync } = require("node:sqlite");
      const db = new DatabaseSync(${JSON.stringify(join(this.dir, "aegis.db"))});
      const now = Date.now();
      const rows = [
        ["aa:bb:cc:00:00:42", "10.9.9.77", "livingroom-tv", now - 3600_000, now - 60_000],
        ["aa:bb:cc:00:00:43", "10.9.9.78", "", now - 7200_000, now - 120_000],
      ];
      for (const row of rows) {
        db.prepare("INSERT OR REPLACE INTO discoveries (mac,address,hostname,first,last) VALUES (?,?,?,?,?)").run(...row);
      }
      // A busy household's query log, so the round-trip numbers mean what a
      // dogfooding operator sees rather than what an empty box can do.
      const insert = db.prepare("INSERT INTO queries (time, client, name, type, verdict, rule) VALUES (?,?,?,?,?,?)");
      const names = ["ads.example.com", "api.spotify.com", "gateway.icloud.com", "graph.facebook.com", "www.youtube.com"];
      const clients = [];
      for (let i = 0; i < 40; i += 1) clients.push("10.9.0." + (i + 2));
      for (let i = 0; i < 20000; i += 1) {
        const time = now - Math.floor(Math.random() * 48 * 3600_000);
        insert.run(time, clients[i % clients.length], names[i % names.length], "A", i % 4 === 0 ? "block" : "allow", "");
      }
    `;
    const seeded = spawnSync(process.execPath, ["--experimental-sqlite", "--no-warnings", "-e", script], { timeout: 10000 });
    if (seeded.status !== 0) {
      throw new Error(`discovery seed failed: ${seeded.stderr}`);
    }
  }

  async down() {
    this.aegis?.kill("SIGTERM");
    await new Promise((resolve) => this.sourceServer?.close(resolve));
    await sleep(150);
    rmSync(this.dir, { recursive: true, force: true });
  }
}

// watchRequests records every /api call the page makes, so the proofs count
// writes instead of trusting the absence of errors. The log is module state so
// a timeout dump can show it.
const liveRequests = [];
function watchRequests(page) {
  const requests = liveRequests;
  void page.send("Network.enable", {});
  page.cdp.on((method, params, session) => {
    if (session !== page.sessionId) {
      return;
    }
    if (method === "Network.requestWillBeSent" && params.request.url.includes("/api/")) {
      requests.push({ url: params.request.url, method: params.request.method });
    }
  });
  return requests;
}

const count = (requests, method, fragment) =>
  requests.filter((entry) => entry.method === method && entry.url.includes(fragment)).length;

// chooseSelect moves a native select to the named option, refocusing before
// each step: the drawer steals focus once shortly after it opens, and a person
// clicking the select would re-focus it too.
async function chooseSelect(page, selector, optionText) {
  const optionsSelector = JSON.stringify(`${selector} option`);
  const wanted = await page.evaluate(`[...document.querySelectorAll(${optionsSelector})].find((o) => o.textContent.trim() === ${JSON.stringify(optionText)})?.value ?? ""`);
  if (wanted === "") {
    throw new Error(`${selector} has no option ${JSON.stringify(optionText)}`);
  }
  for (let step = 0; step < 40; step += 1) {
    await page.focus(selector);
    if ((await page.evaluate(`document.querySelector(${JSON.stringify(selector)}).value`)) === wanted) {
      return;
    }
    await page.key("ArrowDown", { code: "ArrowDown", keyCode: 40 });
    await sleep(40);
  }
  throw new Error(`${selector} never reached ${JSON.stringify(optionText)}`);
}

// clickRowButton presses a row action on the row whose text names the record,
// by real mouse events at the button's coordinates. Synthetic element.click()
// never reaches Solid's delegated handlers.
async function clickRowButton(page, rowText, testid) {
  const box = await page.evaluate(`(() => {
    const tr = [...document.querySelectorAll("tbody tr, [data-testid='client-row'], [data-testid='profile-row']")].find((el) => el.textContent.includes(${JSON.stringify(rowText)}));
    const button = tr?.querySelector('[data-testid="${testid}"]');
    if (!button) return null;
    button.scrollIntoView({ block: "center" });
    const box = button.getBoundingClientRect();
    return { x: box.x + box.width / 2, y: box.y + box.height / 2, row: tr.textContent.slice(0, 60) };
  })()`);
  if (!box) {
    throw new Error(`no ${testid} on the row for ${rowText}`);
  }
  console.log(`clickRowButton ${testid} for ${rowText}: matched row ${JSON.stringify(box.row)} at (${Math.round(box.x)}, ${Math.round(box.y)})`);
  for (const type of ["mousePressed", "mouseReleased"]) {
    await page.send("Input.dispatchMouseEvent", { type, x: box.x, y: box.y, button: "left", clickCount: 1 });
  }
}

// roundTrip is one write measured the way an operator feels it: from the click
// to the moment the visible list shows the result. The 15 ms poll is the
// measurement floor, which is noise at a 1000 ms budget. On a timeout the
// function reports the number anyway and dumps what the page was doing, so a
// slow build still yields its measurement.
async function roundTrip(page, click, until, label, { timeoutMs = 90000 } = {}) {
  await page.evaluate("window.__t0 = performance.now()");
  await click();
  try {
    await page.waitFor(until, { intervalMs: 15, timeoutMs, label });
  } catch (cause) {
    await dumpPage(page, label);
    throw cause;
  }
  return Math.round(await page.evaluate("performance.now() - window.__t0"));
}

// dumpPage prints what the page was mid-doing when a wait gave up: the api
// calls the harness saw, the api timings the browser recorded, and errors.
async function dumpPage(page, label) {
  const summary = {};
  for (const entry of liveRequests.slice(-40)) {
    const key = `${entry.method} ${new URL(entry.url).pathname}`;
    summary[key] = (summary[key] ?? 0) + 1;
  }
  const state = await page.evaluate(
    `(() => ({
      drawer: Boolean(document.querySelector(".drawer")),
      errors: [...document.querySelectorAll("p.error, [role=alert]")].map((node) => node.textContent),
      rewritesTable: document.body.textContent.includes("speaker.local"),
      rows: document.querySelectorAll("tbody tr").length,
    }))()`,
  ).catch(() => ({}));
  console.error(`--- page state at "${label}" ---`);
  console.error(JSON.stringify({ apiCalls: summary, state }, null, 2));
}

async function text(page, selector) {
  return page.evaluate(`document.querySelector(${JSON.stringify(selector)})?.textContent ?? ""`);
}

// typeInto focuses before every character. The drawer focuses its panel once,
// deferred, after it opens, and that steal races a single upfront focus; a
// person clicking the field re-focuses it, and so does this. Punctuation needs
// its physical key code, or Chromium drops the character.
const KEY_CODES = { ".": ["Period", 190], ",": ["Comma", 188], "-": ["Minus", 189], "/": ["Slash", 191] };

async function typeInto(page, selector, value) {
  for (const character of value) {
    await page.focus(selector);
    const special = KEY_CODES[character];
    if (special) {
      await page.key(character, { code: special[0], keyCode: special[1], text: character });
    } else {
      await page.key(character, { text: character, keyCode: character.toUpperCase().charCodeAt(0) });
    }
  }
}

async function clickNodeAt(page, find) {
  const box = await page.evaluate(`(() => {
    const node = [...document.querySelectorAll('[data-testid="screen-constellation"] g[data-id]')].find(${find});
    if (!node) return null;
    const marker = node.querySelector("circle, rect");
    const box = marker.getBoundingClientRect();
    return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
  })()`);
  if (!box) {
    throw new Error("no graph node matched");
  }
  for (const type of ["mousePressed", "mouseReleased"]) {
    await page.send("Input.dispatchMouseEvent", { type, x: box.x, y: box.y, button: "left", clickCount: 1 });
  }
}

const solidNode = (label) =>
  `[...document.querySelectorAll('[data-testid="screen-constellation"] g[data-id]')].some((n) => n.textContent.includes(${JSON.stringify(label)}) && (n.getAttribute("opacity") ?? "1") === "1")`;
const dimNode = (label) =>
  `[...document.querySelectorAll('[data-testid="screen-constellation"] g[data-id]')].some((n) => n.textContent.includes(${JSON.stringify(label)}) && (n.getAttribute("opacity") ?? "1") !== "1")`;

// ── baseline: how long a write takes today ────────────────────────────────

async function baseline() {
  const rig = await new Rig({ bin: BIN }).up();
  const browser = await Browser.launch({ port: rig.ports.devtools });
  try {
    const page = await browser.newPage();
    await page.viewport({ width: 1440, height: 900 });
    await page.open(`${rig.base}/#/services`);
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`);
    const requests = watchRequests(page);
    const results = {};

    const catalog = await (await fetch(`${rig.base}/api/v1/services`)).json();
    const first = catalog.services[0].id;

    const before = await page.evaluate(`document.querySelector('[data-testid="service-${first}"]').checked`);
    results.servicesToggle = await roundTrip(
      page,
      () => page.click(`[data-testid="service-${first}"]`),
      `document.querySelector('[data-testid="service-${first}"]').checked === ${!before}`,
      `service ${first} flips`,
    );
    console.log(`servicesToggle ${results.servicesToggle} ms, ${count(requests, "GET", "/api/")} api GETs, ${count(requests, "PUT", "/services")} PUTs`);
    results.servicesToggleApiGets = count(requests, "GET", "/api/");
    requests.length = 0;

    await page.click('[data-testid="tab-rewrites"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrites-new"]'))`);
    await page.click('[data-testid="rewrites-new"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrite-pattern"]'))`);
    await typeInto(page, '[data-testid="rewrite-pattern"]', "speaker.local");
    await typeInto(page, '[data-testid="rewrite-target"]', "10.9.9.51");
    requests.length = 0;
    results.rewriteCreate = await roundTrip(
      page,
      () => page.click('[data-testid="rewrite-save"]'),
      `document.body.textContent.includes("speaker.local")`,
      "rewrite row appears",
    );
    console.log(`rewriteCreate ${results.rewriteCreate} ms, ${count(requests, "GET", "/api/")} api GETs`);
    results.rewriteCreateApiGets = count(requests, "GET", "/api/");
    requests.length = 0;

    results.rewriteDelete = await roundTrip(
      page,
      async () => {
        await clickRowButton(page, "speaker.local", "rewrite-delete");
        await clickRowButton(page, "speaker.local", "rewrite-delete");
      },
      `!document.body.textContent.includes("speaker.local")`,
      "rewrite row disappears",
    );
    console.log(`rewriteDelete ${results.rewriteDelete} ms, ${count(requests, "GET", "/api/")} api GETs`);

    console.log(JSON.stringify(results, null, 2));
  } finally {
    await browser.close();
    await rig.down();
  }
}

// ── gaps: the three dogfooding proofs ─────────────────────────────────────

async function gaps() {
  const rig = await new Rig({ bin: BIN }).up();
  const browser = await Browser.launch({ port: rig.ports.devtools });
  const failures = [];
  const check = (name, ok, detail) => {
    console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail === undefined ? "" : ` — ${detail}`}`);
    if (!ok) {
      failures.push(name);
    }
  };
  try {
    const page = await browser.newPage();
    await page.viewport({ width: 1440, height: 900 });
    await page.open(`${rig.base}/#/services`);
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`);
    const requests = watchRequests(page);
    const catalog = await (await fetch(`${rig.base}/api/v1/services`)).json();
    const ids = catalog.services.slice(0, 3).map((service) => service.id);
    const savedBadge = `document.querySelector('[data-testid="save-status"]')?.textContent === 'saved'`;

    // ── gap 1: the services page stages, saves once, discards ─────────────
    const checked = (id) => page.evaluate(`document.querySelector('[data-testid="service-${id}"]').checked`);

    await page.click(`[data-testid="service-${ids[0]}"]`);
    await sleep(300);
    check("a flip stages without writing", count(requests, "PUT", "/services") === 0);
    check("a staged flip shows at once", (await checked(ids[0])) === true);

    await page.click(`[data-testid="service-${ids[1]}"]`);
    await page.click(`[data-testid="service-${ids[2]}"]`);
    await sleep(300);
    check("three flips still write nothing", count(requests, "PUT", "/services") === 0);
    const saveLabel = await text(page, '[data-testid="services-save"]');
    check("save names the staged count", /\d/.test(saveLabel), saveLabel);

    requests.length = 0;
    await page.click('[data-testid="services-save"]');
    await page.waitFor(savedBadge, { label: "saved badge" });
    check("one Save is exactly one write", count(requests, "PUT", "/services") === 1, `${count(requests, "PUT", "/services")} PUTs`);
    const stored = await (await fetch(`${rig.base}/api/v1/profiles/default/services`)).json();
    check("the stored set holds every staged service", ids.every((id) => stored.services.includes(id)));

    requests.length = 0;
    await page.click(`[data-testid="service-${ids[0]}"]`);
    await sleep(200);
    await page.click('[data-testid="services-discard"]');
    await sleep(200);
    check("discard reverts the draft", (await checked(ids[0])) === true);
    check("discard writes nothing", count(requests, "PUT", "/services") === 0);

    // One service on the kids profile, so the client scope has something to
    // inherit and lock.
    await page.click('[data-testid="services-scope-profile"]');
    await page.choose('[data-testid="services-scope"]', "kids");
    await sleep(200);
    await page.click(`[data-testid="service-${ids[0]}"]`);
    await sleep(200);
    requests.length = 0;
    await page.click('[data-testid="services-save"]');
    await page.waitFor(savedBadge, { label: "saved badge on the kids profile" });
    check("a profile save is one write", count(requests.filter((entry) => entry.url.includes("/profiles/")), "PUT", "/services") === 1);

    await page.click('[data-testid="services-scope-client"]');
    await page.choose('[data-testid="services-scope"]', "10.9.9.2");
    await sleep(300);
    const locked = await page.evaluate(
      `[...document.querySelectorAll("label.service-card")].some((card) => card.querySelector(".badge.profile") && card.querySelector("input.switch").disabled)`,
    );
    check("inherited sliders stay locked on a client", locked);
    // ids[0] is the inherited one; the draft takes a service the client owns.
    await page.click(`[data-testid="service-${ids[2]}"]`);
    await sleep(200);
    requests.length = 0;
    await page.click('[data-testid="services-save"]');
    await page.waitFor(savedBadge, { label: "saved badge on the client scope" });
    check("a client save is one write", count(requests.filter((entry) => entry.url.includes("/clients/")), "PUT", "/services") === 1);

    // ── gap 2: the constellation draws seen-but-unsaved devices ───────────
    await page.click('[data-testid="tab-constellation"]');
    await page.waitFor(`document.querySelector('[data-testid="screen-constellation"] svg') !== null`, { label: "graph svg" });
    await sleep(1500);

    check("a discovery draws dimmed", await page.evaluate(dimNode("LIVINGROOM-TV")));
    check("an observed address draws dimmed", await page.evaluate(dimNode("127.0.0.1")));

    await clickNodeAt(page, `(n) => n.textContent.includes("LIVINGROOM-TV")`);
    await page.waitFor(`Boolean(document.querySelector('[data-testid="device-claim-name"]'))`, { label: "claim panel" });
    await typeInto(page, '[data-testid="device-claim-name"]', "livingroom-tv");
    requests.length = 0;
    await page.click('[data-testid="device-claim"]');
    await page.waitFor(solidNode("LIVINGROOM-TV"), { timeoutMs: 20000, label: "claimed node turns solid" });
    check("claim creates one client", count(requests, "PUT", "/api/v1/clients/") === 1);

    await clickNodeAt(page, `(n) => n.textContent.includes("LIVINGROOM-TV")`);
    await page.waitFor(`Boolean(document.querySelector('[data-testid="client-remove"]'))`, { label: "remove control" });
    requests.length = 0;
    await page.click('[data-testid="client-remove"]');
    await page.click('[data-testid="client-remove"]');
    await page.waitFor(`!${solidNode("LIVINGROOM-TV")}`, { timeoutMs: 20000, label: "node leaves the graph" });
    check("remove deletes the record", count(requests, "DELETE", "/api/v1/clients/") === 1);
    const clients = await (await fetch(`${rig.base}/api/v1/clients`)).json();
    check("the client is gone from the server", !clients.some((client) => client.name === "livingroom-tv"));

    // ── gap 3: writes land under a second, everywhere ─────────────────────
    const under = async (name, click, until) => {
      const ms = await roundTrip(page, click, until, name);
      check(`${name} lands under a second`, ms > 0 && ms < 1000, `${ms} ms`);
    };

    requests.length = 0;
    await page.click('[data-testid="tab-services"]');
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`);
    await page.click(`[data-testid="service-${ids[1]}"]`);
    await sleep(200);
    await under("services save", () => page.click('[data-testid="services-save"]'), savedBadge);
    check("services save is one write", count(requests, "PUT", "/services") === 1);

    await page.click('[data-testid="tab-rewrites"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrites-new"]'))`);
    await page.click('[data-testid="rewrites-new"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrite-pattern"]'))`);
    await typeInto(page, '[data-testid="rewrite-pattern"]', "lamp.local");
    await typeInto(page, '[data-testid="rewrite-target"]', "10.9.9.52");
    await under("rewrite create", () => page.click('[data-testid="rewrite-save"]'), `document.body.textContent.includes("lamp.local")`);
    await under(
      "rewrite delete",
      async () => {
        await clickRowButton(page, "lamp.local", "rewrite-delete");
        await clickRowButton(page, "lamp.local", "rewrite-delete");
      },
      `!document.body.textContent.includes("lamp.local")`,
    );

    await page.click('[data-testid="tab-clients"]');
    await page.waitFor(`document.querySelectorAll('[data-testid="client-row"]').length > 0`);
    const clientsBefore = await page.evaluate(`document.querySelectorAll('[data-testid="client-row"]').length`);
    await under(
      "client delete",
      async () => {
        await page.click('[data-testid="client-delete"]');
        await page.click('[data-testid="client-delete"]');
      },
      `document.querySelectorAll('[data-testid="client-row"]').length === ${clientsBefore - 1}`,
    );

    await page.click('[data-testid="tab-profiles"]');
    await page.waitFor(`document.querySelectorAll('[data-testid="profile-row"]').length > 0`);
    await page.click('[data-testid="profile-edit"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="profile-mode"]'))`);
    await chooseSelect(page, '[data-testid="profile-mode"]', "refused");
    await under(
      "profile save",
      () => page.click('[data-testid="profile-save"]'),
      `[...document.querySelectorAll('[data-testid="profile-row"]')].some((row) => row.textContent.includes("default") && row.textContent.includes("refused")) && !document.querySelector(".drawer")`,
    );

    // A failed refetch surfaces an error instead of a silent stale list.
    await page.click('[data-testid="tab-rewrites"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrites-new"]'))`);
    await page.click('[data-testid="rewrites-new"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="rewrite-pattern"]'))`);
    await typeInto(page, '[data-testid="rewrite-pattern"]', "film.local");
    await typeInto(page, '[data-testid="rewrite-target"]', "10.9.9.53");
    await page.click('[data-testid="rewrite-save"]');
    await page.waitFor(`document.body.textContent.includes("film.local")`, { label: "film row exists" });
    await under(
      "rewrite delete",
      async () => {
        await clickRowButton(page, "film.local", "rewrite-delete");
        await clickRowButton(page, "film.local", "rewrite-delete");
      },
      `!document.body.textContent.includes("film.local")`,
    );

    // A failed refetch surfaces an error instead of a silent stale list. The
    // clients write reloads the sighting feeds afterwards, so the harness fails
    // exactly that GET: the write lands, the list reflects it, and the reload
    // failure still names itself on screen.
    await page.click('[data-testid="tab-clients"]');
    await page.waitFor(`document.querySelectorAll('[data-testid="client-row"]').length > 0`);
    const beforeFailedDelete = await page.evaluate(`document.querySelectorAll('[data-testid="client-row"]').length`);
    await page.send("Fetch.enable", { patterns: [{ urlPattern: "*clients/observed*" }] });
    const failGets = page.cdp.on((method, params, session) => {
      if (session !== page.sessionId || method !== "Fetch.requestPaused") {
        return;
      }
      if (params.request.method === "GET") {
        void page.send("Fetch.failRequest", { requestId: params.requestId, errorReason: "Failed" });
      } else {
        void page.send("Fetch.continueRequest", { requestId: params.requestId });
      }
    });
    try {
      await under(
        "client delete under a failed refetch",
        async () => {
          await clickRowButton(page, "devices-01", "client-delete");
          await clickRowButton(page, "devices-01", "client-delete");
        },
        `document.querySelectorAll('[data-testid="client-row"]').length === ${beforeFailedDelete - 1}`,
      );
      await page.waitFor(`Boolean(document.querySelector('[role="alert"]'))`, { timeoutMs: 15000, label: "refetch failure surfaces" });
      const alert = await text(page, '[role="alert"]');
      check("a failed refetch surfaces an error", alert.includes("reloading the lists failed"), alert.slice(0, 90));
    } finally {
      failGets();
      await page.send("Fetch.disable", {});
    }

    if (failures.length > 0) {
      throw new Error(`${failures.length} checks failed: ${failures.join("; ")}`);
    }
    console.log("ALL CHECKS PASSED");
  } finally {
    await browser.close();
    await rig.down();
  }
}

// ── prove: the save reaches the filter, and a wipe leaves a working server ──

async function prove() {
  const rig = await new Rig({ bin: BIN }).up();
  const browser = await Browser.launch({ port: rig.ports.devtools });
  const failures = [];
  const check = (name, ok, detail) => {
    console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail === undefined ? "" : ` — ${detail}`}`);
    if (!ok) {
      failures.push(name);
    }
  };

  // One real DNS question, read the way an operator reads it: the rcode plus
  // whatever the answer section carries.
  const dig = (name) => {
    const run = spawnSync("dig", ["+time=3", "+tries=1", "-p", String(rig.ports.dns), "@127.0.0.1", name, "A"], { timeout: 15000 });
    const answer = run.stdout.toString();
    const status = /status: (\w+)/.exec(answer)?.[1] ?? "no answer";
    const section = /;; ANSWER SECTION:\n([\s\S]*?)\n\n/.exec(answer)?.[1] ?? "";
    return { status, answers: section.split("\n").filter(Boolean) };
  };

  // The log flushes once a second, so a verdict is waited for rather than
  // assumed, and a caller asks for the row it means: the newest row for a name
  // is the question just asked, but only once the flush carried it.
  const rowFor = async (name, matches) => {
    for (let attempt = 0; attempt < 80; attempt += 1) {
      const log = await (await fetch(`${rig.base}/api/v1/queries?limit=20`)).json();
      const row = log.queries.find((entry) => entry.name === name && matches(entry));
      if (row) {
        return row;
      }
      await sleep(250);
    }
    return null;
  };

  try {
    // The catalog is the input a save needs, so the run waits for the stored
    // copy instead of racing the boot fetch.
    await waitUntil(
      async () => (await (await fetch(`${rig.base}/api/v1/services`)).json()).services.length > 0,
      "the services catalog is stored",
    );

    // One setting in place before the browser opens, so the Settings page has
    // a tag to show and the wipe has a list to empty. It goes in the denied
    // list: a non-empty allowed list serves only what it names, which would
    // shut the harness's own queries out.
    const stored = await fetch(`${rig.base}/api/v1/access`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allowed: [], disallowed: ["192.0.2.1/32"] }),
    });
    check("an access list is stored before the wipe", stored.ok);

    const page = await browser.newPage();
    await page.viewport({ width: 1440, height: 900 });
    await page.open(`${rig.base}/#/services`);
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`);
    const requests = watchRequests(page);

    const catalog = await (await fetch(`${rig.base}/api/v1/services`)).json();
    const reddit = catalog.services.find((service) => service.id === "reddit");
    check("the catalog carries reddit", Boolean(reddit), JSON.stringify(catalog.services.slice(0, 3).map((service) => service.id)));
    if (!reddit) {
      throw new Error("no reddit in the catalog, so there is nothing to save");
    }

    // ── save then block ─────────────────────────────────────────────────────
    const before = dig("reddit.com");
    check("reddit.com is let through before the save", before.status !== "NXDOMAIN", `status ${before.status}, ${before.answers.length} answers`);
    const beforeRow = await rowFor("reddit.com", (entry) => entry.verdict === "allow");
    check("the log agrees the query was allowed", Boolean(beforeRow), JSON.stringify(beforeRow));

    await page.click(`[data-testid="service-${reddit.id}"]`);
    await page.waitFor(`document.querySelector('[data-testid="services-save"]') !== null`);
    await page.click('[data-testid="services-save"]');
    await page.waitFor(`document.querySelector('[data-testid="save-status"]')?.textContent === 'saved'`, { label: "the save lands" });

    const after = dig("reddit.com");
    check("reddit.com is blocked after the save", after.status === "NXDOMAIN", `status ${after.status}`);
    const afterRow = await rowFor("reddit.com", (entry) => entry.verdict === "block" && String(entry.rule ?? "").startsWith(`${reddit.id}:`));
    check("the block carries the service rule", Boolean(afterRow), JSON.stringify(afterRow));

    // ── the full-wipe round trip ────────────────────────────────────────────
    const clientsBefore = (await (await fetch(`${rig.base}/api/v1/clients`)).json()).length;
    const discoveriesBefore = (await (await fetch(`${rig.base}/api/v1/discoveries`)).json()).discoveries.length;
    const queriesBefore = (await (await fetch(`${rig.base}/api/v1/queries?limit=1`)).json()).queries.length;
    check(
      "the box holds something before the wipe",
      clientsBefore > 0 && discoveriesBefore > 0 && queriesBefore > 0,
      `${clientsBefore} clients, ${discoveriesBefore} discoveries, ${queriesBefore}+ queries`,
    );

    await page.click('[data-testid="tab-system"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="tab-settings"]'))`, { label: "the system subnav" });
    await page.click('[data-testid="tab-settings"]');
    await page.waitFor(`Boolean(document.querySelector('[data-testid="settings-reset"]'))`, { label: "the reset button" });
    check("the access tag is on screen", await page.evaluate(`document.querySelector('[data-testid="access-deny-tag"]') !== null`));

    requests.length = 0;
    await page.click('[data-testid="settings-reset"]');
    await sleep(300);
    const armed = await text(page, '[data-testid="settings-reset"]');
    check("the first press arms and sends nothing", count(requests, "POST", "/api/v1/reset") === 0 && armed.includes("Confirm wipe"), armed);

    await page.click('[data-testid="settings-reset"]');
    await waitUntil(async () => (await (await fetch(`${rig.base}/api/v1/clients`)).json()).length === 0, "the wipe lands");
    check("one wipe, one request", count(requests, "POST", "/api/v1/reset") === 1, `${count(requests, "POST", "/api/v1/reset")} POSTs`);

    const clientsAfter = (await (await fetch(`${rig.base}/api/v1/clients`)).json()).length;
    const discoveriesAfter = (await (await fetch(`${rig.base}/api/v1/discoveries`)).json()).discoveries.length;
    const queriesAfter = (await (await fetch(`${rig.base}/api/v1/queries?limit=1`)).json()).queries.length;
    const servicesAfter = (await (await fetch(`${rig.base}/api/v1/services`)).json()).services.length;
    const accessAfter = await (await fetch(`${rig.base}/api/v1/access`)).json();
    check("clients are gone", clientsAfter === 0, `${clientsAfter} left`);
    check("discoveries are gone", discoveriesAfter === 0, `${discoveriesAfter} left`);
    check("the query log is gone", queriesAfter === 0, `${queriesAfter} left`);
    check("the services catalog is gone", servicesAfter === 0, `${servicesAfter} left`);
    check("settings are gone", accessAfter.allowed.length === 0 && accessAfter.disallowed.length === 0, JSON.stringify(accessAfter));

    await page.waitFor(`document.querySelector('[data-testid="access-deny-tag"]') === null`, { label: "the access tag leaves the screen" });
    check("the page shows the empty state", await page.evaluate(`document.body.textContent.includes("nobody is refused")`));

    // The wiped server still answers: the blocklist file it booted with is
    // untouched, and a name nothing blocks still leaves for upstream.
    const stillBlocks = dig("ads.example.com");
    check("the wiped server still blocks", stillBlocks.status === "NXDOMAIN", `status ${stillBlocks.status}`);
    const stillServes = dig("example.com");
    check("the wiped server still serves", stillServes.status !== "NXDOMAIN", `status ${stillServes.status}`);

    // And the save path still works on it: one press brings the catalog back,
    // and the slider blocks again.
    await page.click('[data-testid="tab-services"]');
    await page.waitFor(`document.body.textContent.includes("No services in the catalog yet")`, { label: "the services page is empty" });
    await page.click('[data-testid="services-refresh"]');
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`, { label: "the catalog comes back", timeoutMs: 30000 });
    await page.click(`[data-testid="service-${reddit.id}"]`);
    await page.waitFor(`document.querySelector('[data-testid="services-save"]') !== null`);
    await page.click('[data-testid="services-save"]');
    await page.waitFor(`document.querySelector('[data-testid="save-status"]')?.textContent === 'saved'`, { label: "the save lands again" });
    const again = dig("reddit.com");
    check("save-then-block works on the wiped server", again.status === "NXDOMAIN", `status ${again.status}`);

    if (failures.length > 0) {
      throw new Error(`${failures.length} checks failed: ${failures.join("; ")}`);
    }
    console.log("ALL CHECKS PASSED");
  } finally {
    await browser.close();
    await rig.down();
  }
}

const command = process.argv[2] ?? "baseline";
if (command === "baseline") {
  await baseline();
} else if (command === "gaps") {
  await gaps();
} else if (command === "prove") {
  await prove();
} else if (command === "shots") {
  const out = process.argv[3] ?? "/tmp/opencode/gap";
  const rig = await new Rig({ bin: BIN }).up();
  const browser = await Browser.launch({ port: rig.ports.devtools });
  try {
    const page = await browser.newPage();
    await page.viewport({ width: 1440, height: 900 });
    await page.open(`${rig.base}/#/services`);
    await page.waitFor(`document.querySelectorAll('[data-testid^="service-"]').length > 0`);
    const catalog = await (await fetch(`${rig.base}/api/v1/services`)).json();
    const ids = catalog.services.slice(0, 3).map((service) => service.id);
    for (const id of ids) {
      await page.click(`[data-testid="service-${id}"]`);
      await sleep(120);
    }
    await page.shot(`${out}/services-staged.png`);
    await page.click('[data-testid="tab-constellation"]');
    await page.waitFor(`document.querySelector('[data-testid="screen-constellation"] svg') !== null`);
    await sleep(2500);
    await page.shot(`${out}/constellation.png`);
    console.log(`shots in ${out}`);
  } finally {
    await browser.close();
    await rig.down();
  }
} else {
  console.error(`unknown command ${command}`);
  process.exit(2);
}
