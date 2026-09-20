import type { BlockedService } from "./api";

export type ServiceGroup = {
  group: string;
  services: BlockedService[];
};

// groupServices orders the catalog the way the API lists its groups, with the
// services that name no group last, so the profile screen keeps one order
// between loads.
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

// toggled returns the set with one service added or removed, which is the whole
// body the profile services endpoint replaces.
export function toggled(current: string[], id: string): string[] {
  return current.includes(id) ? current.filter((service) => service !== id) : [...current, id];
}
