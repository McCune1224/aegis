// @vitest-environment jsdom
import { render } from "@solidjs/web";
import { flush } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import CrudActions from "./CrudActions";

function mount() {
   document.body.innerHTML = "";
   const host = document.createElement("div");
   document.body.appendChild(host);
   let deleted = 0;
   let edited = 0;
   const dispose = render(
      () => (
         <CrudActions
            testid="rule"
            onEdit={() => (edited += 1)}
            onDuplicate={() => {}}
            onDelete={() => (deleted += 1)}
         />
      ),
      host,
   );
   flush();
   const press = (testid: string) => {
      host.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)?.click();
      flush();
   };
   return { host, dispose, press, deleted: () => deleted, edited: () => edited };
}

describe("CrudActions delete", () => {
   it("arms on the first press and deletes on the second", () => {
      const view = mount();
      const button = view.host.querySelector<HTMLButtonElement>('[data-testid="rule-delete"]');

      view.press("rule-delete");
      expect(view.deleted()).toBe(0);
      expect(button?.textContent).toBe("Confirm delete");

      view.press("rule-delete");
      expect(view.deleted()).toBe(1);
      view.dispose();
   });

   it("disarms itself so a stray second click elsewhere deletes nothing", () => {
      vi.useFakeTimers();
      const view = mount();
      const button = view.host.querySelector<HTMLButtonElement>('[data-testid="rule-delete"]');

      view.press("rule-delete");
      expect(button?.textContent).toBe("Confirm delete");

      vi.advanceTimersByTime(4000);
      flush();
      expect(button?.textContent).toBe("Delete");

      view.press("rule-delete");
      expect(view.deleted()).toBe(0);
      vi.useRealTimers();
      view.dispose();
   });

   it("edits on the first press, so arming never delays the other verbs", () => {
      const view = mount();
      view.press("rule-edit");
      expect(view.edited()).toBe(1);
      view.dispose();
   });

   it("disables every verb while a save is in flight", () => {
      document.body.innerHTML = "";
      const host = document.createElement("div");
      document.body.appendChild(host);
      const dispose = render(() => <CrudActions testid="rule" busy={true} onEdit={() => {}} onDuplicate={() => {}} onDelete={() => {}} />, host);
      flush();
      const buttons = host.querySelectorAll<HTMLButtonElement>('[data-testid^="rule-"]');
      expect([...buttons].every((button) => button.disabled)).toBe(true);
      dispose();
   });

   it("names the row so a column of Delete buttons is not ambiguous", () => {
      document.body.innerHTML = "";
      const host = document.createElement("div");
      document.body.appendChild(host);
      const dispose = render(
         () => <CrudActions testid="rule" label="ads.example.com" onEdit={() => {}} onDuplicate={() => {}} onDelete={() => {}} />,
         host,
      );
      flush();
      const del = host.querySelector<HTMLButtonElement>('[data-testid="rule-delete"]');
      expect(del?.getAttribute("aria-label")).toBe("Delete ads.example.com");
      del?.click();
      flush();
      expect(host.querySelector<HTMLButtonElement>('[data-testid="rule-delete"]')?.getAttribute("aria-label")).toBe(
         "Confirm delete ads.example.com",
      );
      dispose();
   });
});
