// @vitest-environment jsdom
import { createSignal, flush } from "solid-js";
import { render } from "@solidjs/web";
import type { JSX } from "@solidjs/web";
import { describe, expect, it } from "vitest";
import type { ScheduleInput } from "./api";
import BlockedServices from "./BlockedServices";
import Clients from "./Clients";
import Profiles from "./Profiles";
import Rewrites from "./Rewrites";
import Rules from "./Rules";
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

type ServicesProps = Parameters<typeof BlockedServices>[0];

// servicesPage is one page with data already loaded, so a test states only what
// it changes: the schedules, the save it wants to watch, or both.
function servicesPage(over?: Partial<ServicesProps>): ServicesProps {
   return {
      services: [{ id: "discord", name: "Discord", group: "Chat", rule_count: 3, profiles: ["kids"], clients: [] }],
      serviceGroups: ["Chat"],
      profileNames: ["kids"],
      clients: [client],
      defaultProfile: "kids",
      schedules: [schedule],
      windows: [window],
      rules: [],
      onSave: noop,
      onRefreshServices: noop,
      onSaveWindow: noop,
      onDeleteWindow: noop,
      onSaveSchedule: noop,
      onDeleteSchedule: noop,
      ...over,
   };
}

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

   it("Blocked Services schedules: edit, duplicate, delete", () => {
      const host = mount(() => <BlockedServices {...servicesPage()} />);
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
      const host = mount(() => <BlockedServices {...servicesPage()} />);
      expect(button(host, "window-edit")).toBeTruthy();
      expect(button(host, "window-duplicate")).toBeTruthy();
      expect(button(host, "window-delete")).toBeTruthy();
   });
});

describe("the services page owns the schedules a window reads", () => {
   function submit(host: HTMLElement, testid: string): void {
      button(host, testid).form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      flush();
   }

   function type(host: HTMLElement, testid: string, text: string): void {
      const field = host.querySelector<HTMLInputElement>(`[data-testid="${testid}"]`);
      if (!field) {
         throw new Error(`no ${testid} in the rendered page`);
      }
      field.value = text;
      field.dispatchEvent(new Event("input", { bubbles: true }));
      flush();
   }

   it("duplicates a schedule under a free name and saves it as that name", async () => {
      const saved: [string, ScheduleInput][] = [];
      const host = mount(() => (
         <BlockedServices
            {...servicesPage({
               schedules: [schedule, { name: "night-copy", priority: 2, windows: [] }],
               onSaveSchedule: async (name, input) => {
                  saved.push([name, input]);
               },
            })}
         />
      ));

      button(host, "schedule-duplicate").click();
      flush();
      expect(drawerTitle(host)).toBe("Duplicate night");
      expect(value(host, "schedule-name")).toBe("night-copy-2");

      submit(host, "schedule-save");
      expect(saved).toEqual([["night-copy-2", { priority: 1, windows: [{ days: [1], start: "21:00", end: "07:00" }] }]]);
   });

   it("creates a schedule from the page without visiting another tab", async () => {
      const saved: string[] = [];
      const host = mount(() => (
         <BlockedServices
            {...servicesPage({
               schedules: [],
               onSaveSchedule: async (name) => {
                  saved.push(name);
               },
            })}
         />
      ));

      expect(host.querySelector('[data-testid="schedules-empty"]')).toBeTruthy();
      button(host, "schedule-add").click();
      flush();
      expect(drawerTitle(host)).toBe("New schedule");

      type(host, "schedule-name", "video-time");
      button(host, "schedule-add-window").click();
      flush();
      submit(host, "schedule-save");
      expect(saved).toEqual(["video-time"]);
   });

   it("opens the schedule editor from the window form and comes back to it", () => {
      const host = mount(() => <BlockedServices {...servicesPage({ schedules: [] })} />);

      button(host, "window-new").click();
      flush();
      expect(drawerTitle(host)).toBe("New window");

      button(host, "window-new-schedule").click();
      flush();
      expect(drawerTitle(host)).toBe("New schedule");

      host.querySelector<HTMLButtonElement>(".drawer-head .icon-btn")?.click();
      flush();
      expect(drawerTitle(host)).toBe("New window");
   });

   it("shows why a schedule a window still names cannot be deleted", async () => {
      const host = mount(() => (
         <BlockedServices
            {...servicesPage({
               onDeleteSchedule: async () => {
                  throw new Error(`schedule night is still named by window "video-time"`);
               },
            })}
         />
      ));

      button(host, "schedule-delete").click();
      flush();
      button(host, "schedule-delete").click();
      await new Promise((resolve) => setTimeout(resolve, 0));
      flush();
      expect(host.querySelector('[data-testid="schedule-error"]')?.textContent).toContain('window "video-time"');
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
      return servicesPage({
         services: [{ id: "chatgpt", name: "ChatGPT", group: "Artificial intelligence", rule_count: 1, profiles: [], clients: [] }],
         serviceGroups: ["Artificial intelligence"],
         profileNames: names,
         clients: [],
         defaultProfile: names[0] ?? "",
         schedules: [],
         windows: [],
         onSave,
      });
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
