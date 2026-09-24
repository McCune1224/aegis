import type { BlockedService } from "./api";

export type ServiceGroup = {
  group: string;
  services: BlockedService[];
};

// groupServices orders the catalog the way the API lists its groups, with the
// services that name no group last, so the blocked-services screen keeps one
// order between loads.
export function groupServices(services: BlockedService[], groups: string[]): ServiceGroup[] {
  const byGroup = new Map<string, BlockedService[]>();
  for (const service of services) {
    const list = byGroup.get(service.group) ?? [];
    list.push(service);
    byGroup.set(service.group, list);
  }

  const ordered: ServiceGroup[] = [];
  for (const group of [...groups, ""]) {
    const list = byGroup.get(group);
    if (!list || list.length === 0) {
      continue;
    }
    ordered.push({ group, services: list });
    byGroup.delete(group);
  }
  for (const [group, list] of byGroup) {
    ordered.push({ group, services: list });
  }
  return ordered;
}

// groupLabels gives the catalog's group ids the names the blocked-services
// page shows. Ids the catalog gains later fall back to a readable rendering,
// so a new category never appears as a raw identifier.
const groupLabels: Record<string, string> = {
  ai: "Artificial intelligence",
  cdn: "Content delivery networks",
  dating: "Dating services",
  gambling: "Gambling and betting",
  gaming: "Gaming",
  hosting: "Web hosting",
  messenger: "Messengers",
  privacy: "Privacy",
  shopping: "Shopping",
  social_network: "Social networks",
  software: "Software and development",
  streaming: "Streaming",
};

export function serviceGroupLabel(group: string): string {
  const known = groupLabels[group];
  if (known) {
    return known;
  }
  if (group === "") {
    return "Other";
  }
  const words = group.split(/[_\s-]+/).filter(Boolean);
  return words.map((word) => word[0].toUpperCase() + word.slice(1)).join(" ");
}

// toggled returns the set with one service added or removed, which is the whole
// body the profile services endpoint replaces.
export function toggled(current: string[], id: string): string[] {
  return current.includes(id) ? current.filter((service) => service !== id) : [...current, id];
}

// GroupState is how much of one catalog group is selected, which is what a
// category toggle reads and shows.
export type GroupState = "none" | "some" | "all";

export function groupState(ids: string[], selected: string[]): GroupState {
  const count = ids.filter((id) => selected.includes(id)).length;
  if (count === 0) {
    return "none";
  }
  if (count === ids.length) {
    return "all";
  }
  return "some";
}

// toggledGroup turns a category toggle into a selection: every id in the group
// is added unless they are all already selected, in which case every id is
// removed. The result keeps the order selected earlier and drops duplicates.
export function toggledGroup(current: string[], ids: string[]): string[] {
  if (groupState(ids, current) === "all") {
    return current.filter((id) => !ids.includes(id));
  }
  return [...new Set([...current, ...ids])];
}
