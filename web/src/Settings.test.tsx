// @vitest-environment jsdom
import { render } from "@solidjs/web";
import { flush } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import type { JSX } from "@solidjs/web";
import Settings from "./Settings";

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

async function settle(): Promise<void> {
   await new Promise((resolve) => setTimeout(resolve, 0));
   flush();
}

type Props = Parameters<typeof Settings>[0];

function page(over?: Partial<Props>): Props {
   return {
      profiles: [{ name: "default" }],
      defaultProfile: "default",
      onSetDefault: async () => {},
      access: { allowed: [], disallowed: [] },
      onSaveAccess: async () => {},
      windowMinutes: 60,
      onSetWindow: () => {},
      live: true,
      onSetLive: () => {},
      onReset: async () => {},
      ...over,
   };
}

describe("the full wipe on the Settings page", () => {
   it("arms on the first press, cancels, and wipes once on the second", async () => {
      const onReset = vi.fn(async () => {});
      const host = mount(() => <Settings {...page({ onReset })} />);

      expect(button(host, "settings-reset").textContent).toContain("Wipe everything");

      button(host, "settings-reset").click();
      flush();
      expect(onReset).not.toHaveBeenCalled();
      expect(button(host, "settings-reset").textContent).toContain("Confirm wipe");

      button(host, "settings-reset-cancel").click();
      flush();
      expect(onReset).not.toHaveBeenCalled();
      expect(button(host, "settings-reset").textContent).toContain("Wipe everything");

      button(host, "settings-reset").click();
      flush();
      button(host, "settings-reset").click();
      await settle();
      expect(onReset).toHaveBeenCalledTimes(1);
      expect(button(host, "settings-reset").textContent).toContain("Wipe everything");
   });

   it("shows what the server refused and keeps the press armed for a retry", async () => {
      const host = mount(() =>
         <Settings
            {...page({
               onReset: async () => {
                  throw new Error("the store is read-only");
               },
            })}
         />,
      );

      button(host, "settings-reset").click();
      flush();
      button(host, "settings-reset").click();
      await settle();

      expect(host.querySelector('[data-testid="settings-reset-error"]')?.textContent).toContain("read-only");
      expect(button(host, "settings-reset").textContent).toContain("Confirm wipe");
   });
});
