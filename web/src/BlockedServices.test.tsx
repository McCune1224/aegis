// @vitest-environment jsdom
import { render } from "@solidjs/web";
import { flush } from "solid-js";
import { describe, expect, it } from "vitest";
import type { JSX } from "@solidjs/web";
import type { BlockedService, ServiceWindow } from "./api";
import BlockedServices, { type ServiceScope } from "./BlockedServices";

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

function input(host: HTMLElement, testid: string): HTMLInputElement {
   const found = host.querySelector<HTMLInputElement>(`[data-testid="${testid}"]`);
   if (!found) {
      throw new Error(`no ${testid} in the rendered page`);
   }
   return found;
}

async function settle(): Promise<void> {
   await new Promise((resolve) => setTimeout(resolve, 0));
   flush();
}

const catalog: BlockedService[] = [
   { id: "discord", name: "Discord", group: "Chat", rule_count: 3, profiles: [], clients: [] },
   { id: "youtube", name: "YouTube", group: "Video", rule_count: 5, profiles: ["kids"], clients: [] },
   { id: "spotify", name: "Spotify", group: "Music", rule_count: 2, profiles: [], clients: [] },
];

type Props = Parameters<typeof BlockedServices>[0];

function page(over?: Partial<Props>): Props {
   return {
      services: catalog,
      serviceGroups: ["Chat", "Video", "Music"],
      profileNames: ["kids"],
      clients: [{ name: "phone", profile: "kids", notes: "", addresses: ["10.9.9.2"], macs: [], prefixes: [] }],
      defaultProfile: "kids",
      schedules: [],
      windows: [] as ServiceWindow[],
      rules: [],
      onSave: async () => {},
      onRefreshServices: async () => {},
      onSaveWindow: async () => {},
      onDeleteWindow: async () => {},
      onSaveSchedule: async () => {},
      onDeleteSchedule: async () => {},
      ...over,
   };
}

type Save = [kind: ServiceScope["kind"], name: string, services: string[]];

function recorder(saves: Save[]) {
   return async (scope: ServiceScope, services: string[]) => {
      saves.push([scope.kind, scope.name, [...services]]);
   };
}

describe("Blocked Services stages flips into one save", () => {
   it("a flip stages locally: no write until Save, then one write with the whole set", async () => {
      const saves: Save[] = [];
      const host = mount(() => <BlockedServices {...page({ onSave: recorder(saves) })} />);

      button(host, "service-discord").click();
      flush();
      expect(saves).toEqual([]);
      expect(input(host, "service-discord").checked).toBe(true);
      expect(button(host, "services-save").textContent).toBe("Save 1 change");

      button(host, "service-spotify").click();
      flush();
      expect(saves).toEqual([]);
      expect(button(host, "services-save").textContent).toBe("Save 2 changes");

      button(host, "services-save").click();
      await settle();
      expect(saves).toEqual([["profile", "kids", ["youtube", "discord", "spotify"]]]);
      expect(host.querySelector('[data-testid="services-save"]')).toBeNull();
   });

   it("Discard returns the sliders to the stored set and writes nothing", async () => {
      const saves: Save[] = [];
      const host = mount(() => <BlockedServices {...page({ onSave: recorder(saves) })} />);

      button(host, "service-discord").click();
      flush();
      button(host, "services-discard").click();
      flush();
      expect(saves).toEqual([]);
      expect(input(host, "service-discord").checked).toBe(false);
      expect(host.querySelector('[data-testid="services-save"]')).toBeNull();
      expect(host.querySelector('[data-testid="services-discard"]')).toBeNull();
   });

   it("a group Block all stages the group and Unblock all stages a removal", async () => {
      const saves: Save[] = [];
      const host = mount(() => <BlockedServices {...page({ onSave: recorder(saves) })} />);

      const chatBlockAll = [...host.querySelectorAll<HTMLButtonElement>("[data-testid^='group-block-']")][0];
      chatBlockAll.click();
      flush();
      expect(saves).toEqual([]);
      expect(button(host, "services-save").textContent).toBe("Save 1 change");
      button(host, "services-save").click();
      await settle();
      expect([saves[0][0], saves[0][1], [...saves[0][2]].sort()]).toEqual(["profile", "kids", ["discord", "youtube"]]);

      button(host, "services-unblock-all").click();
      flush();
      expect(saves).toHaveLength(1);
      // The fixture's stored set does not advance, so from the page's view one
      // staged change remains: dropping the one server-blocked service.
      expect(button(host, "services-save").textContent).toBe("Save 1 change");
      button(host, "services-save").click();
      await settle();
      expect(saves[1]).toEqual(["profile", "kids", []]);
   });

   it("a failed save keeps the draft and shows what went wrong", async () => {
      const host = mount(() =>
         <BlockedServices
            {...page({
               onSave: async () => {
                  throw new Error("the store is read-only");
               },
            })}
         />,
      );

      button(host, "service-discord").click();
      flush();
      button(host, "services-save").click();
      await settle();
      expect(host.querySelector("p.error")?.textContent).toContain("read-only");
      expect(input(host, "service-discord").checked).toBe(true);
      expect(button(host, "services-save").textContent).toBe("Save 1 change");
   });

   it("the staged draft waits on its own scope and survives a scope round trip", async () => {
      const saves: Save[] = [];
      const host = mount(() => <BlockedServices {...page({ onSave: recorder(saves) })} />);

      button(host, "service-discord").click();
      flush();
      button(host, "services-scope-client").click();
      flush();
      expect(host.querySelector('[data-testid="services-save"]')).toBeNull();

      button(host, "services-scope-profile").click();
      flush();
      expect(button(host, "services-save").textContent).toBe("Save 1 change");
      expect(saves).toEqual([]);
   });

   it("an inherited slider stays locked on a client and never joins its draft", async () => {
      const saves: Save[] = [];
      const host = mount(() => <BlockedServices {...page({ onSave: recorder(saves) })} />);
      button(host, "services-scope-client").click();
      flush();

      const inherited = input(host, "service-youtube");
      expect(inherited.disabled).toBe(true);
      expect(inherited.checked).toBe(true);

      button(host, "service-discord").click();
      flush();
      button(host, "services-save").click();
      await settle();
      expect(saves).toEqual([["client", "phone", ["discord"]]]);
   });
});
