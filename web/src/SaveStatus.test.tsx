// @vitest-environment jsdom
import { render } from "@solidjs/web";
import { createSignal, flush } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import SaveStatus from "./SaveStatus";
import type { SaveState } from "./crud";

function mount(initial: SaveState) {
   document.body.innerHTML = "";
   const host = document.createElement("div");
   document.body.appendChild(host);
   const [state, setState] = createSignal<SaveState>(initial);
   const dispose = render(() => <SaveStatus state={state()} />, host);
   flush();
   return { host, setState, dispose, badge: () => host.querySelector('[data-testid="save-status"]') };
}

describe("SaveStatus", () => {
   it("shows nothing while nothing has been written", () => {
      const view = mount("idle");
      expect(view.badge()).toBeNull();
      view.dispose();
   });

   it("announces the write in flight with aria-busy", () => {
      const view = mount("saving");
      const badge = view.badge();
      expect(badge?.textContent).toBe("saving…");
      expect(badge?.getAttribute("aria-busy")).toBe("true");
      expect(badge?.getAttribute("role")).toBe("status");
      view.dispose();
   });

   it("shows saved once the write lands and clears itself", () => {
      const view = mount("idle");
      vi.useFakeTimers();
      view.setState("saved");
      flush();
      expect(view.badge()?.textContent).toBe("saved");
      expect(view.badge()?.getAttribute("aria-busy")).toBe("false");

      vi.advanceTimersByTime(2500);
      flush();
      expect(view.badge()).toBeNull();
      vi.useRealTimers();
      view.dispose();
   });

   it("drops the saved badge as soon as the next write starts", () => {
      const view = mount("saved");
      flush();
      expect(view.badge()?.textContent).toBe("saved");

      view.setState("saving");
      flush();
      expect(view.badge()?.textContent).toBe("saving…");
      view.dispose();
   });
});
