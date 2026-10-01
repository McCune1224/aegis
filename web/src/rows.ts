// The two moves a visible list makes when a write lands: one row rewritten in
// place, one row gone. Every write handler in the console goes through these,
// so a save reflects without a refetch and no page grows its own copy.

export function upsertBy<T>(rows: T[], row: T, key: (row: T) => string): T[] {
   const id = key(row);
   const index = rows.findIndex((candidate) => key(candidate) === id);
   if (index === -1) {
      return [...rows, row];
   }
   return rows.map((candidate, position) => (position === index ? row : candidate));
}

export function dropBy<T>(rows: T[], key: (row: T) => string, id: string): T[] {
   return rows.filter((candidate) => key(candidate) !== id);
}

// applyServiceScope rewrites one scope's column of the services catalog from a
// saved whole-answer set, so the sliders are exact without refetching the
// catalog the write did not touch.
export function applyServiceScope<T extends { id: string; profiles: string[]; clients: string[] }>(
   rows: T[],
   kind: "profile" | "client",
   name: string,
   services: string[],
): T[] {
   const saved = new Set(services);
   const column = kind === "profile" ? "profiles" : "clients";
   return rows.map((row) => {
      const others = row[column].filter((entry) => entry !== name);
      return { ...row, [column]: saved.has(row.id) ? [...others, name].sort() : others };
   });
}
