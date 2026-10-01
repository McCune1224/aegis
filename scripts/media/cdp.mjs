// A dependency-free Chrome DevTools Protocol driver on Node's built-in WebSocket.
// The README images are captured from the real binary in a real browser, so the
// only thing needed here is a way to drive one. Node ships WebSocket, Chromium
// ships the protocol, and nothing else is required.

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const CHROMIUM = process.env.AEGIS_CHROMIUM ?? "/usr/bin/chromium-browser";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// codeFor names the physical key a character is typed on, which is what
// Chromium reports a keyboard event's code as.
function codeFor(character) {
  if (/^[a-z]$/i.test(character)) {
    return `Key${character.toUpperCase()}`;
  }
  if (/^[0-9]$/.test(character)) {
    return `Digit${character}`;
  }
  if (character === " ") {
    return "Space";
  }
  return character;
}

async function waitForDevTools(port, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/json/version`);
      if (response.ok) {
        return await response.json();
      }
    } catch {
      // The listener is not up yet.
    }
    await sleep(100);
  }
  throw new Error(`devtools on port ${port} never answered`);
}

class Connection {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 1;
    this.pending = new Map();
    this.listeners = new Set();
    socket.addEventListener("message", (event) => this.receive(String(event.data)));
  }

  receive(text) {
    const message = JSON.parse(text);
    if (message.id !== undefined) {
      const waiter = this.pending.get(message.id);
      if (!waiter) {
        return;
      }
      this.pending.delete(message.id);
      if (message.error) {
        waiter.reject(new Error(`${message.error.message} (${JSON.stringify(message.error.data ?? "")})`));
      } else {
        waiter.resolve(message.result);
      }
      return;
    }
    for (const listener of this.listeners) {
      listener(message.method, message.params ?? {}, message.sessionId);
    }
  }

  send(method, params = {}, sessionId) {
    const id = this.nextId++;
    const payload = { id, method, params };
    if (sessionId) {
      payload.sessionId = sessionId;
    }
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.socket.send(JSON.stringify(payload));
    });
  }

  on(listener) {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  once(method, sessionId, timeoutMs = 30000) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        off();
        reject(new Error(`no ${method} within ${timeoutMs}ms`));
      }, timeoutMs);
      const off = this.on((name, params, session) => {
        if (name === method && (sessionId === undefined || session === sessionId)) {
          clearTimeout(timer);
          off();
          resolve(params);
        }
      });
    });
  }
}

// Page is one browser tab. Every method is the CDP command it wraps, with the
// waiting and the defaults a screenshot pass needs.
export class Page {
  constructor(connection, sessionId) {
    this.cdp = connection;
    this.sessionId = sessionId;
    this.problems = [];
  }

  send(method, params) {
    return this.cdp.send(method, params, this.sessionId);
  }

  async viewport({ width, height, scale = 2, mobile = false }) {
    await this.send("Emulation.setDeviceMetricsOverride", {
      width,
      height,
      deviceScaleFactor: scale,
      mobile,
      screenWidth: width,
      screenHeight: height,
    });
  }

  // open navigates and waits for the new document to be interactive. The load
  // event is armed before the navigate so it cannot be missed, but a same
  // document hash change or a cached asset can settle before the listener runs,
  // so readyState is the condition that decides.
  async open(url) {
    const loaded = this.cdp.once("Page.loadEventFired", this.sessionId, 45000).catch(() => undefined);
    const result = await this.send("Page.navigate", { url });
    if (result.errorText) {
      throw new Error(`navigating to ${url}: ${result.errorText}`);
    }
    await loaded;
    await this.waitFor("document.readyState === 'complete' || document.readyState === 'interactive'");
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (result.exceptionDetails) {
      throw new Error(`evaluate failed: ${result.exceptionDetails.text} ${result.exceptionDetails.exception?.description ?? ""}`);
    }
    return result.result.value;
  }

  // waitFor polls the page for an expression to come true. Solid renders after
  // fetch, so a selector or a rendered count is the honest readiness signal.
  async waitFor(expression, { timeoutMs = 20000, intervalMs = 100, label = expression } = {}) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (await this.evaluate(`Boolean(${expression})`)) {
        return;
      }
      await sleep(intervalMs);
    }
    throw new Error(`page never satisfied: ${label}`);
  }

  // key sends a real key event. Synthetic dispatchEvent never reaches a delegated
  // Solid handler, and a native select only commits a choice on a real arrow
  // key carrying the matching code, so both `code` and the virtual key code are
  // filled in. Text characters get keyDown so the character is delivered;
  // everything else gets rawKeyDown, which is what Chromium expects for a key
  // with no printable text.
  async key(key, { code, keyCode, text } = {}) {
    const base = {
      key,
      code: code ?? codeFor(key),
      windowsVirtualKeyCode: keyCode ?? 0,
      nativeVirtualKeyCode: keyCode ?? 0,
      ...(text ? { text } : {}),
    };
    await this.send("Input.dispatchKeyEvent", { type: text ? "keyDown" : "rawKeyDown", ...base });
    await this.send("Input.dispatchKeyEvent", { type: "keyUp", ...base });
  }

  // choose moves a native select to the option with the given text, by focusing
  // it and stepping with arrow keys until the value matches.
  // choose moves a native select to the named option. Focus comes from
  // element.focus() rather than a click: in headless Chromium a click on a
  // select opens its popup instead of focusing it, and the arrow keys then go to
  // the popup. The keys are real Input events with a matching code, which is
  // what a native select listens for, and the loop stops as soon as the value
  // lands on the wanted one.
  async choose(selector, optionText) {
    // The whole selector, options included, goes into one string: interpolating
    // the selector and appending a bare `option` outside the quotes is not
    // valid JavaScript.
    const optionsSelector = JSON.stringify(`${selector} option`);
    const wanted = await this.evaluate(`[...document.querySelectorAll(${optionsSelector})].find((o) => o.textContent.trim() === ${JSON.stringify(optionText)})?.value ?? ""`);
    if (wanted === "") {
      throw new Error(`${selector} has no option ${JSON.stringify(optionText)}`);
    }
    await this.focus(selector);
    for (let step = 0; step < 40; step += 1) {
      if ((await this.evaluate(`document.querySelector(${JSON.stringify(selector)}).value`)) === wanted) {
        return;
      }
      await this.key("ArrowDown", { keyCode: 40 });
      await sleep(40);
    }
    throw new Error(`${selector} never reached ${JSON.stringify(optionText)}`);
  }

  async focus(selector) {
    const focused = await this.evaluate(`document.querySelector(${JSON.stringify(selector)})?.focus(), document.activeElement?.tagName`);
    if (!focused || focused === "BODY") {
      throw new Error(`could not focus ${selector}`);
    }
  }

  // type writes into a field the way a person does: focus it, then one real
  // key event per character, because a delegated Solid handler ignores
  // synthetic input.
  async type(selector, text) {
    await this.focus(selector);
    for (const character of text) {
      await this.key(character, { text: character, keyCode: character.toUpperCase().charCodeAt(0) });
      await sleep(20);
    }
  }

  // click presses an element by selector, or the first one whose text contains
  // the given string. The text form is how a capture picks a named row rather
  // than whichever row happens to sort first.
  async click(selector, { text } = {}) {
    const box = await this.evaluate(`(() => {
      const nodes = [...document.querySelectorAll(${JSON.stringify(selector)})];
      const element = ${text === undefined ? "nodes[0]" : `nodes.find((node) => node.textContent.includes(${JSON.stringify(text)}))`};
      if (!element) return null;
      element.scrollIntoView({ block: "center" });
      const rect = element.getBoundingClientRect();
      return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
    })()`);
    if (!box) {
      throw new Error(`no element for ${selector}${text === undefined ? "" : ` containing ${JSON.stringify(text)}`}`);
    }
    for (const type of ["mousePressed", "mouseReleased"]) {
      await this.send("Input.dispatchMouseEvent", { type, x: box.x, y: box.y, button: "left", clickCount: 1 });
    }
  }

  async height() {
    const metrics = await this.send("Page.getLayoutMetrics");
    return Math.ceil(metrics.cssContentSize.height);
  }

  // shot writes a PNG. fullPage captures the whole scroll height, which for a
  // console whose status line is pinned to the viewport means a tall image with
  // dead paper under it, so the default is the viewport an operator sees.
  async shot(path, { fullPage = false } = {}) {
    const clip = fullPage ? { ...(await this.contentBox()), scale: 1 } : undefined;
    const result = await this.send("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: fullPage,
      ...(clip ? { clip } : {}),
    });
    const { writeFileSync, mkdirSync } = await import("node:fs");
    mkdirSync(join(path, ".."), { recursive: true });
    writeFileSync(path, Buffer.from(result.data, "base64"));
    return path;
  }

  async contentBox() {
    const metrics = await this.send("Page.getLayoutMetrics");
    const size = metrics.cssContentSize;
    return { x: 0, y: 0, width: Math.ceil(size.width), height: Math.ceil(size.height) };
  }
}

// Browser owns the Chromium process and the tabs it opens.
export class Browser {
  static async launch({ port = 9333, headless = true } = {}) {
    const profile = mkdtempSync(join(tmpdir(), "aegis-media-"));
    const args = [
      headless ? "--headless=new" : "--start-maximized",
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${profile}`,
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--hide-scrollbars",
      "--force-device-scale-factor=1",
      "--force-color-profile=srgb",
      "--disable-lcd-text",
      // The Constellation view renders to a WebGL canvas. Software rasterization
      // is the only GPU a build box has, and it needs the flag to say so.
      "--enable-unsafe-swiftshader",
      "--use-gl=angle",
      "--use-angle=swiftshader",
      "about:blank",
    ];
    const child = spawn(CHROMIUM, args, { stdio: ["ignore", "pipe", "pipe"] });
    const noise = [];
    child.stdout.on("data", (chunk) => noise.push(String(chunk)));
    child.stderr.on("data", (chunk) => noise.push(String(chunk)));
    const version = await waitForDevTools(port, 20000);
    const socket = new WebSocket(version.webSocketDebuggerUrl);
    await new Promise((resolve, reject) => {
      socket.addEventListener("open", resolve, { once: true });
      socket.addEventListener("error", reject, { once: true });
    });
    return new Browser(child, new Connection(socket), profile, noise);
  }

  constructor(child, connection, profile, noise) {
    this.child = child;
    this.cdp = connection;
    this.profile = profile;
    this.noise = noise;
  }

  async newPage() {
    const { targetId } = await this.cdp.send("Target.createTarget", { url: "about:blank" });
    const { sessionId } = await this.cdp.send("Target.attachToTarget", { targetId, flatten: true });
    const page = new Page(this.cdp, sessionId);
    await page.send("Page.enable", {});
    await page.send("Runtime.enable", {});
    await page.send("Log.enable", {});
    this.cdp.on((method, params, session) => {
      if (session !== sessionId) {
        return;
      }
      if (method === "Log.entryAdded" && params.entry.level === "error") {
        // The URL travels with the text, so a bare message is never a mystery.
        // A browser asks for /favicon.ico on its own and the console serves
        // none: that one request is the browser's, not the app's, so it is
        // named and excluded rather than silenced everywhere.
        const text = `${params.entry.text} ${params.entry.url ?? ""}`.trim();
        if (!text.includes("/favicon.ico")) {
          page.problems.push(text);
        }
      }
      if (method === "Runtime.exceptionThrown") {
        page.problems.push(params.exceptionDetails.text);
      }
    });
    return page;
  }

  async close() {
    try {
      this.cdp.socket.close();
    } catch {
      // Already closed.
    }
    this.child.kill("SIGKILL");
    await sleep(200);
    rmSync(this.profile, { recursive: true, force: true });
  }
}