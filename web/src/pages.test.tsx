// @vitest-environment jsdom
import { createSignal, flush } from "solid-js";
import { render } from "@solidjs/web";
import type { JSX } from "@solidjs/web";
import { describe, expect, it } from "vitest";
import BlockedServices from "./BlockedServices";
import Clients from "./Clients";
import Profiles from "./Profiles";
import Rewrites from "./Rewrites";
import Rules from "./Rules";
import Schedules from "./Schedules";
import Sources from "./Sources";
import Upstreams from "./Upstreams";

const noop = async () => {};

// mount renders a page the way App does, into a detached container, and flushes
// the write that render scheduled. The body is cleared first so one test's
// drawer can never satisfy another test's lookup.
function mount(page: () => JSX.Element): HTMLElement {
   document.body.innerHTML = "";
   const host = document.createElement("div");
   document.body.appendChild(host);
   render(page, host);
   flush();
   return host;
}

function button(host: HTMLElement, testid: string): HTMLButtonElement {
   const found = host.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`);
   if (!found) {
      throw new Error(`no ${testid} in the rendered page`);
   }
   return found;
}

function drawerTitle(host: HTMLElement): string {
   const heading = host.querySelector(".drawer-head h2");
   return heading?.textContent?.trim() ?? "";
}

function value(host: HTMLElement, testid: string): string {
   return host.querySelector<HTMLInputElement>(`[data-testid="${testid}"]`)?.value ?? "";
}

const profile = { name: "kids", mode: "refused" };
const client = { name: "phone", profile: "kids", notes: "", addresses: ["10.9.9.2"], macs: [], prefixes: [] };
const schedule = { name: "night", priority: 1, windows: [{ days: [1], start: "21:00", end: "07:00" }] };
const window = { name: "video-time", action: "block" as const, schedule: "night", clients: ["phone"], services: ["discord"] };

describe("every list page offers the same verbs", () => {
   it("Rules: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Rules
            rules={[{ id: 7, domain: "ads.example.com", kind: "exact", action: "block" }]}
            schedules={["night"]}
            clients={["phone"]}
            onCreate={noop}
            onUpdate={noop}
            onDelete={noop}
         />
      ));
      expect(button(host, "rule-edit").textContent).toBe("Edit");
      expect(button(host, "rule-duplicate").textContent).toBe("Duplicate");
      expect(button(host, "rule-delete").textContent).toBe("Delete");
   });

   it("Schedules: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Schedules schedules={[schedule]} rules={[]} onSave={noop} onDelete={noop} />
      ));
      expect(button(host, "schedule-edit")).toBeTruthy();
      expect(button(host, "schedule-duplicate")).toBeTruthy();
      expect(button(host, "schedule-delete")).toBeTruthy();
   });

   it("Rewrites: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Rewrites rewrites={[{ pattern: "nas.local", target: "192.168.1.50" }]} onSave={noop} onDelete={noop} />
      ));
      expect(button(host, "rewrite-edit")).toBeTruthy();
      expect(button(host, "rewrite-duplicate")).toBeTruthy();
      expect(button(host, "rewrite-delete")).toBeTruthy();
   });

   it("Sources: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Sources
            sources={[
               {
                  name: "stevenblack",
                  url: "https://example.com/hosts",
                  format: "hosts",
                  enabled: true,
                  rule_count: 10,
                  skipped: 0,
                  failures: 0,
                  refresh_seconds: 0,
               },
            ]}
            catalog={[]}
            onSave={noop}
            onDelete={noop}
            onReload={noop}
         />
      ));
      expect(button(host, "source-edit")).toBeTruthy();
      expect(button(host, "source-duplicate")).toBeTruthy();
      expect(button(host, "source-delete")).toBeTruthy();
   });

   it("Profiles: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Profiles
            profiles={[profile, { name: "strict" }]}
            defaultProfile="kids"
            safesearch={[]}
            onSave={noop}
            onDelete={noop}
            onSetDefault={noop}
            onSaveSafesearch={noop}
         />
      ));
      expect(button(host, "profile-edit")).toBeTruthy();
      expect(button(host, "profile-duplicate")).toBeTruthy();
      expect(button(host, "profile-delete")).toBeTruthy();
   });

   it("Clients: edit, duplicate, delete", () => {
      const host = mount(() => (
         <Clients
            clients={[client]}
            profiles={[profile]}
            discoveries={[]}
            observed={[]}
            onClaimObserved={noop}
            onClaimDiscovery={noop}
            onDismissDiscovery={noop}
            onSave={noop}
            onDelete={noop}
         />
      ));
      expect(button(host, "client-edit")).toBeTruthy();
      expect(button(host, "client-duplicate")).toBeTruthy();
      expect(button(host, "client-delete")).toBeTruthy();
   });

   it("Upstreams: edit, duplicate, delete for an upstream and a route", () => {
      const host = mount(() => (
         <Upstreams
            upstreams={[
               { name: "quad9", url: "9.9.9.9:53", enabled: true, backup: false, latency_ms: 12, failures: 0, down: false },
            ]}
            routes={[{ id: 1, domain: "ads.example.com", client: "", upstream: "quad9" }]}
            onSaveUpstream={noop}
            onDeleteUpstream={noop}
            onCreateRoute={noop}
            onUpdateRoute={noop}
            onDeleteRoute={noop}
         />
      ));
      expect(button(host, "upstream-edit")).toBeTruthy();
      expect(button(host, "upstream-duplicate")).toBeTruthy();
      expect(button(host, "upstream-delete")).toBeTruthy();
      expect(button(host, "route-edit")).toBeTruthy();
      expect(button(host, "route-duplicate")).toBeTruthy();
      expect(button(host, "route-delete")).toBeTruthy();
   });

   it("Blocked Services windows: edit, duplicate, delete", () => {
      const host = mount(() => (
         <BlockedServices
            services={[{ id: "discord", name: "Discord", group: "Chat", rule_count: 3, profiles: ["kids"], clients: [] }]}
            serviceGroups={["Chat"]}
            profileNames={["kids"]}
            clients={[client]}
            defaultProfile="kids"
            schedules={[schedule]}
            windows={[window]}
            onSave={noop}
            onRefreshServices={noop}
            onSaveWindow={noop}
            onDeleteWindow={noop}
         />
      ));
      expect(button(host, "window-edit")).toBeTruthy();
      expect(button(host, "window-duplicate")).toBeTruthy();
      expect(button(host, "window-delete")).toBeTruthy();
   });
});

describe("duplicate copies the row under a free key", () => {
   it("Rules: duplicate opens the drawer with the row's fields", () => {
      const host = mount(() => (
         <Rules
            rules={[{ id: 7, domain: "ads.example.com", kind: "wildcard", action: "allow" }]}
            schedules={[]}
            clients={[]}
            onCreate={noop}
            onUpdate={noop}
            onDelete={noop}
         />
      ));
      button(host, "rule-duplicate").click();
      flush();
      expect(drawerTitle(host)).toBe("Duplicate rule");
      expect(value(host, "rule-domain")).toBe("");
      expect(value(host, "rule-new-action")).toBe("allow");
   });

   it("Profiles: duplicate opens the drawer under a free name", () => {
      const host = mount(() => (
         <Profiles
            profiles={[{ name: "kids" }, { name: "kids-copy" }]}
            defaultProfile="kids"
            safesearch={[]}
            onSave={noop}
            onDelete={noop}
            onSetDefault={noop}
            onSaveSafesearch={noop}
         />
      ));
      button(host, "profile-duplicate").click();
      flush();
      expect(drawerTitle(host)).toBe("Duplicate kids");
      expect(value(host, "profile-name")).toBe("kids-copy-2");
      expect(host.querySelector<HTMLInputElement>('[data-testid="profile-name"]')?.disabled).toBe(false);
   });
});

describe("Blocked Services save feedback", () => {
   function servicesProps(names: string[], onSave: () => Promise<void>) {
      return {
         services: [{ id: "chatgpt", name: "ChatGPT", group: "Artificial intelligence", rule_count: 1, profiles: [], clients: [] }],
         serviceGroups: ["Artificial intelligence"],
         profileNames: names,
         clients: [],
         defaultProfile: names[0] ?? "",
         schedules: [],
         windows: [],
         onSave,
         onRefreshServices: noop,
         onSaveWindow: noop,
         onDeleteWindow: noop,
      };
   }

   it("adopts a scope once the first load lands", () => {
      document.body.innerHTML = "";
      const host = document.createElement("div");
      document.body.appendChild(host);
      const [names, setNames] = createSignal<string[]>([]);
      render(() => <BlockedServices {...servicesProps(names(), noop)} />, host);
      flush();

      expect(host.querySelector('[data-testid="scope-note"]')?.textContent).toContain("blocked for every client on .");

      setNames(["default"]);
      flush();
      expect(host.querySelector('[data-testid="scope-note"]')?.textContent).toContain("blocked for every client on default.");
   });

   it("explains the write while it travels and reports the result", async () => {
      document.body.innerHTML = "";
      const host = document.createElement("div");
      document.body.appendChild(host);
      let release = () => {};
      const gate = new Promise<void>((resolve) => {
         release = resolve;
      });
      render(() => <BlockedServices {...servicesProps(["default"], () => gate)} />, host);
      flush();

      const checkbox = host.querySelector<HTMLInputElement>('[data-testid="service-chatgpt"]');
      checkbox?.click();
      flush();

      const status = host.querySelector('[data-testid="save-status"]');
      expect(status?.textContent).toBe("saving…");
      expect(status?.getAttribute("aria-busy")).toBe("true");
      expect(checkbox?.disabled).toBe(true);

      release();
      await gate;
      flush();
      expect(host.querySelector('[data-testid="save-status"]')?.textContent).toBe("saved");
      expect(checkbox?.disabled).toBe(false);
   });
});
