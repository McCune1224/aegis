// Tab deep links live in the URL hash, so a refresh or a shared link lands
// on the view the operator left. The shape is #/<tab> and #/system/<view>.

import type { SystemView, Tab } from "./App";

export type AppRoute = { tab: Tab; system: SystemView };

const TAB_IDS: readonly Tab[] = [
  "dashboard",
  "constellation",
  "log",
  "clients",
  "profiles",
  "rules",
  "schedules",
  "rewrites",
  "services",
  "system",
];

const SYSTEM_IDS: readonly SystemView[] = ["upstreams", "sources", "settings"];

const DEFAULT_ROUTE: AppRoute = { tab: "dashboard", system: "upstreams" };

function isTab(value: string | undefined): value is Tab {
  return TAB_IDS.includes(value as Tab);
}

function isSystem(value: string | undefined): value is SystemView {
  return SYSTEM_IDS.includes(value as SystemView);
}

export function parseAppHash(hash: string): AppRoute {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean);
  const [head, sub] = parts;
  if (!isTab(head)) {
    return DEFAULT_ROUTE;
  }
  if (head === "system") {
    return { tab: "system", system: isSystem(sub) ? sub : "upstreams" };
  }
  return { tab: head, system: DEFAULT_ROUTE.system };
}

export function appHashFor(tab: Tab, system: SystemView): string {
  if (tab === "system") {
    return `#/system/${system}`;
  }
  return tab === "dashboard" ? "#/" : `#/${tab}`;
}
