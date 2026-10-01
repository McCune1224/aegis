import { flush } from "solid-js";
import { describe, expect, it } from "vitest";
import {
   closeEditor,
   createCrud,
   duplicateName,
   editorTitle,
   openDuplicate,
   openEdit,
   openNew,
} from "./crud";

describe("editor state", () => {
   it("opens new, edit, and duplicate with the key the row carries", () => {
      expect(openNew<string>()).toEqual({ mode: "new" });
      expect(openEdit("kids")).toEqual({ mode: "edit", key: "kids" });
      expect(openDuplicate("kids")).toEqual({ mode: "duplicate", key: "kids" });
      expect(closeEditor()).toEqual({ mode: "closed" });
   });

   it("titles the drawer from the state so every page reads the same", () => {
      expect(editorTitle(openNew<string>(), "profile")).toBe("New profile");
      expect(editorTitle(openEdit("kids"), "profile")).toBe("Edit kids");
      expect(editorTitle(openDuplicate("kids"), "profile")).toBe("Duplicate kids");
   });
});

describe("duplicateName", () => {
   it("appends -copy to a free name", () => {
      expect(duplicateName("kids", ["home", "strict"])).toBe("kids-copy");
   });

   it("counts up while the name is taken", () => {
      expect(duplicateName("kids", ["kids-copy", "kids-copy-2"])).toBe("kids-copy-3");
   });

   it("skips a gap in the taken suffixes without reusing a live name", () => {
      expect(duplicateName("kids", ["kids-copy-2"])).toBe("kids-copy");
   });
});

describe("createCrud", () => {
   it("moves through the states and records a failed run as the drawer error", async () => {
      const crud = createCrud<string>();

      crud.openDuplicate("kids");
      flush();
      expect(crud.editor()).toEqual({ mode: "duplicate", key: "kids" });

      await crud.run(async () => {
         throw new Error("name already taken");
      });
      expect(crud.error()).toBe("Error: name already taken");
      expect(crud.busy()).toBe(false);

      crud.close();
      flush();
      expect(crud.editor()).toEqual({ mode: "closed" });
   });

   it("reports saving while a run is in flight and saved once it lands", async () => {
      const crud = createCrud<string>();
      expect(crud.saveState()).toBe("idle");

      let release = () => {};
      const inFlight = new Promise<void>((resolve) => {
         release = resolve;
      });
      const run = crud.run(() => inFlight);
      flush();
      expect(crud.saveState()).toBe("saving");
      expect(crud.busy()).toBe(true);

      release();
      await run;
      flush();
      expect(crud.saveState()).toBe("saved");
      expect(crud.busy()).toBe(false);
   });

   it("leaves no saved badge behind when the run fails", async () => {
      const crud = createCrud<string>();
      await crud.run(async () => {
         throw new Error("refused");
      });
      flush();
      expect(crud.saveState()).toBe("idle");
      expect(crud.error()).toBe("Error: refused");
   });
});
