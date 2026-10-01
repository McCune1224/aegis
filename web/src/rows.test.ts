import { describe, expect, it } from "vitest";
import { applyServiceScope, dropBy, upsertBy } from "./rows";
import type { BlockedService, Client } from "./api";

const phone: Client = { name: "phone", profile: "kids", notes: "", addresses: ["10.9.9.2"], macs: [], prefixes: [] };
const pad: Client = { name: "pad", profile: "kids", notes: "", addresses: ["10.9.9.3"], macs: [], prefixes: [] };

describe("upsertBy", () => {
   it("replaces a row in place and keeps the order", () => {
      const changed = { ...phone, notes: "new" };
      expect(upsertBy([phone, pad], changed, (client) => client.name)).toEqual([changed, pad]);
   });

   it("appends a row the list has never seen", () => {
      const third = { ...pad, name: "third" };
      expect(upsertBy([phone], third, (client) => client.name)).toEqual([phone, third]);
   });

   it("appends to an empty list", () => {
      expect(upsertBy<Client>([], phone, (client) => client.name)).toEqual([phone]);
   });
});

describe("dropBy", () => {
   it("removes only the named row", () => {
      expect(dropBy([phone, pad], (client) => client.name, "phone")).toEqual([pad]);
   });

   it("keeps every row when the key names none of them", () => {
      expect(dropBy([phone, pad], (client) => client.name, "ghost")).toEqual([phone, pad]);
   });
});

describe("applyServiceScope", () => {
   const catalog: BlockedService[] = [
      { id: "discord", name: "Discord", group: "Chat", rule_count: 1, profiles: ["kids"], clients: ["phone"] },
      { id: "youtube", name: "YouTube", group: "Video", rule_count: 2, profiles: [], clients: [] },
   ];

   it("rewrites the profile column of every service in the saved set", () => {
      const rows = applyServiceScope(catalog, "profile", "kids", ["youtube"]);
      expect(rows.find((row) => row.id === "discord")?.profiles).toEqual([]);
      expect(rows.find((row) => row.id === "youtube")?.profiles).toEqual(["kids"]);
      expect(rows.find((row) => row.id === "youtube")?.clients).toEqual([]);
   });

   it("rewrites the client column when the scope is one client", () => {
      const rows = applyServiceScope(catalog, "client", "phone", ["youtube"]);
      expect(rows.find((row) => row.id === "youtube")?.clients).toEqual(["phone"]);
      expect(rows.find((row) => row.id === "discord")?.clients).toEqual([]);
      expect(rows.find((row) => row.id === "discord")?.profiles).toEqual(["kids"]);
   });
});
