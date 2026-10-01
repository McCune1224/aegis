// @vitest-environment jsdom
import { render } from "@solidjs/web";
import { flush } from "solid-js";
import { describe, expect, it } from "vitest";
import Drawer from "./Drawer";

function mountDrawer() {
   document.body.innerHTML = "";
   const host = document.createElement("div");
   document.body.appendChild(host);
   let closed = 0;
   const dispose = render(
      () => (
         <Drawer open={true} title="New profile" onClose={() => (closed += 1)}>
            <p>body</p>
         </Drawer>
      ),
      host,
   );
   flush();
   return { dispose, count: () => closed };
}

function pressEscape() {
   window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
}

describe("Drawer Escape", () => {
   it("closes the open drawer", () => {
      const drawer = mountDrawer();
      pressEscape();
      expect(drawer.count()).toBe(1);
      drawer.dispose();
   });

   it("stops listening once the drawer is unmounted", () => {
      const drawer = mountDrawer();
      drawer.dispose();
      pressEscape();
      expect(drawer.count()).toBe(0);
   });
});
