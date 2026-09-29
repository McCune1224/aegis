import type { JSX } from "@solidjs/web";
import { createEffect, createSignal, Show } from "solid-js";
import {
  createRoute as postRoute,
  createRule as postRule,
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  deleteRewrite as removeRewrite,
  deleteRoute as removeRoute,
  deleteRule as removeRule,
  deleteSchedule as removeSchedule,
  deleteSource as removeSource,
  deleteUpstream as removeUpstream,
  getAccess,
  getDefaultProfile,
  getStatus,
  listCatalog,
  listClients,
  listProfiles,
  listRewrites,
  listRoutes,
  listRules,
  listWindows,
  listSchedules,
  listSafesearch,
  listDiscoveries,
  dismissDiscovery as removeDiscovery,
  listThreatFindings,
  listServices,
  listSources,
  listUpstreams,
  refreshServices as postRefreshServices,
  saveClient as putClient,
  saveClientServices as putClientServices,
  saveProfile as putProfile,
  saveProfileSafesearch as putProfileSafesearch,
  saveProfileServices as putProfileServices,
  saveRewrite as putRewrite,
  saveWindow as putWindow,
  deleteWindow as deleteWindowByName,
  saveSchedule as putSchedule,
  saveSource as putSource,
  saveUpstream as putUpstream,
  saveAccess as putAccess,
  setDefaultProfile as putDefaultProfile,
  updateRoute as patchRoute,
  updateRule as patchRule,
  type CatalogEntry,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
  type BlockedService,
  type Discovery,
  type SafesearchEngine,
  type ThreatFinding,
  type Rewrite,
  type Route,
  type RouteInput,
  type Rule,
  type ServiceWindow,
  type ServiceWindowInput,
  type RuleInput,
  type Schedule,
  type ScheduleInput,
  type Source,
  type SourceInput,
  type Upstream,
  type UpstreamInput,
  type AccessSettings,
} from "./api";
import {
  IconClients,
  IconClock,
  IconConstellation,
  IconDashboard,
  IconGear,
  IconLog,
  IconRewrite,
  IconRules,
  IconServices,
  IconShield,
  IconSources,
  IconUpstream,
} from "./Icons";
import BlockedServices, { type ServiceScope } from "./BlockedServices";
import Clients from "./Clients";
import Dashboard from "./Dashboard";
import Graph from "./Graph";
import QueryLog from "./QueryLog";
import Settings from "./Settings";
import { createQueryLog } from "./querylog";
import Profiles from "./Profiles";
import Rewrites from "./Rewrites";
import Rules from "./Rules";
import Schedules from "./Schedules";
import Sources from "./Sources";
import Upstreams from "./Upstreams";
import { starField } from "./sky";

type Tab =
  | "dashboard"
  | "constellation"
  | "log"
  | "clients"
  | "services"
  | "profiles"
  | "rules"
  | "schedules"
  | "rewrites"
  | "upstreams"
  | "sources"
  | "settings";

type NavItem = { id: Tab; label: string; icon: () => JSX.Element };

const NAV_GROUPS: { label: string; items: NavItem[] }[] = [
  {
    label: "Observe",
    items: [
      { id: "dashboard", label: "Overview", icon: IconDashboard },
      { id: "constellation", label: "Constellation", icon: IconConstellation },
      { id: "log", label: "Query Log", icon: IconLog },
    ],
  },
  {
    label: "Policy",
    items: [
      { id: "clients", label: "Clients", icon: IconClients },
      { id: "profiles", label: "Profiles", icon: IconShield },
      { id: "rules", label: "Rules", icon: IconRules },
      { id: "schedules", label: "Schedules", icon: IconClock },
    ],
  },
  {
    label: "Controls",
    items: [
      { id: "services", label: "Blocked Services", icon: IconServices },
      { id: "rewrites", label: "Rewrites", icon: IconRewrite },
    ],
  },
  {
    label: "Network",
    items: [
      { id: "upstreams", label: "Upstreams", icon: IconUpstream },
      { id: "sources", label: "Sources", icon: IconSources },
    ],
  },
  {
    label: "System",
    items: [{ id: "settings", label: "Settings", icon: IconGear }],
  },
];

const TITLES: Record<Tab, string> = {
  dashboard: "Overview",
  constellation: "Constellation",
  log: "Query Log",
  clients: "Clients",
  services: "Blocked Services",
  profiles: "Profiles",
  rules: "Rules",
  schedules: "Schedules",
  rewrites: "Rewrites",
  upstreams: "Upstreams",
  sources: "Sources",
  settings: "Settings",
};

// The sky is seeded, so every load paints the same night.
const sky = starField();

export default function App() {
  const [tab, setTab] = createSignal<Tab>("dashboard");
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [sources, setSources] = createSignal<Source[]>([]);
  const [catalog, setCatalog] = createSignal<CatalogEntry[]>([]);
  const [services, setServices] = createSignal<BlockedService[]>([]);
  const [serviceGroups, setServiceGroups] = createSignal<string[]>([]);
  const [safesearch, setSafesearch] = createSignal<SafesearchEngine[]>([]);
  const [discoveries, setDiscoveries] = createSignal<Discovery[]>([]);
  const [threats, setThreats] = createSignal<ThreatFinding[]>([]);
  const [rules, setRules] = createSignal<Rule[]>([]);
  const [schedules, setSchedules] = createSignal<Schedule[]>([]);
  const [windows, setWindows] = createSignal<ServiceWindow[]>([]);
  const [rewrites, setRewrites] = createSignal<Rewrite[]>([]);
  const [upstreamRows, setUpstreamRows] = createSignal<Upstream[]>([]);
  const [routes, setRoutes] = createSignal<Route[]>([]);
  const [access, setAccess] = createSignal<AccessSettings>({ allowed: [], disallowed: [] });
  const [defaultProfile, setDefaultProfile] = createSignal("");
  const [upstreams, setUpstreams] = createSignal<string[]>([]);
  const [ruleCount, setRuleCount] = createSignal(0);
  const [error, setError] = createSignal<string>();
  const [windowMinutes, setWindowMinutes] = createSignal(Number(localStorage.getItem("aegis.window")) || 60);
  const [live, setLive] = createSignal(localStorage.getItem("aegis.live") !== "0");
  const [logFilter, setLogFilter] = createSignal<{ client?: string; name?: string }>({});

  const log = createQueryLog({ live: live() });

  async function refresh() {
    const [nextProfiles, nextClients, nextSources, nextCatalog, nextRules, nextSchedules, nextWindows, nextRewrites, nextUpstreams, nextRoutes, nextAccess, nextDefault, nextStatus, nextServices, nextSafesearch, nextDiscoveries, nextThreats] =
      await Promise.all([
        listProfiles(),
        listClients(),
        listSources(),
        listCatalog(),
        listRules(),
        listSchedules(),
        listWindows(),
        listRewrites(),
        listUpstreams(),
        listRoutes(),
        getAccess(),
        getDefaultProfile(),
        getStatus(),
        listServices(),
        listSafesearch(),
        listDiscoveries(),
        listThreatFindings(),
      ]);
    setProfiles(nextProfiles);
    setClients(nextClients);
    setSources(nextSources);
    setCatalog(nextCatalog);
    setRules(nextRules);
    setSchedules(nextSchedules);
    setWindows(nextWindows);
    setRewrites(nextRewrites);
    setUpstreamRows(nextUpstreams);
    setRoutes(nextRoutes);
    setAccess(nextAccess);
    setDefaultProfile(nextDefault.profile);
    setUpstreams(nextStatus.upstreams);
    setRuleCount(nextStatus.rules ?? 0);
    setServices(nextServices.services);
    setServiceGroups(nextServices.groups);
    setSafesearch(nextSafesearch.engines);
    setDiscoveries(nextDiscoveries.discoveries);
    setThreats(nextThreats.findings);
  }

  createEffect(
    () => undefined,
    () => {
      void refresh().catch((cause) => setError(String(cause)));
      void log.load({ limit: 2000 }).catch((cause) => setError(String(cause)));
    },
  );

  async function saveProfile(name: string, input: ProfileInput) {
    await putProfile(name, input);
    await refresh();
  }

  async function saveClient(name: string, input: ClientInput) {
    await putClient(name, input);
    await refresh();
  }

  async function deleteProfile(name: string) {
    await removeProfile(name);
    await refresh();
  }

  async function deleteClient(name: string) {
    await removeClient(name);
    await refresh();
  }

  async function saveSource(name: string, input: SourceInput) {
    await putSource(name, input);
    await refresh();
  }

  async function deleteSource(name: string) {
    await removeSource(name);
    await refresh();
  }

  async function addRule(input: RuleInput) {
    await postRule(input);
    await refresh();
  }

  async function changeRule(id: number, input: RuleInput) {
    await patchRule(id, input);
    await refresh();
  }

  async function deleteRule(id: number) {
    await removeRule(id);
    await refresh();
  }

  async function addSchedule(name: string, input: ScheduleInput) {
    await putSchedule(name, input);
    await refresh();
  }

  async function deleteSchedule(name: string) {
    await removeSchedule(name);
    await refresh();
  }

  async function addWindow(name: string, input: ServiceWindowInput) {
    await putWindow(name, input);
    await refresh();
  }

  async function deleteServiceWindow(name: string) {
    await deleteWindowByName(name);
    await refresh();
  }

  async function saveRewrite(pattern: string, target: string) {
    await putRewrite(pattern, target);
    await refresh();
  }

  async function deleteRewrite(pattern: string) {
    await removeRewrite(pattern);
    await refresh();
  }

  async function saveUpstream(name: string, input: UpstreamInput) {
    await putUpstream(name, input);
    await refresh();
  }

  async function deleteUpstream(name: string) {
    await removeUpstream(name);
    await refresh();
  }

  async function addRoute(input: RouteInput) {
    await postRoute(input);
    await refresh();
  }

  async function changeRoute(id: number, input: RouteInput) {
    await patchRoute(id, input);
    await refresh();
  }

  async function deleteRoute(id: number) {
    await removeRoute(id);
    await refresh();
  }

  async function makeDefault(name: string) {
    await putDefaultProfile(name);
    await refresh();
  }

  async function saveClientServices(scope: ServiceScope, serviceIDs: string[]) {
    if (scope.kind === "client") {
      await putClientServices(scope.name, serviceIDs);
    } else {
      await putProfileServices(scope.name, serviceIDs);
    }
    await refresh();
  }

  async function refreshServiceCatalog() {
    await postRefreshServices();
    await refresh();
  }

  async function saveProfileSafesearch(name: string, engines: string[]) {
    await putProfileSafesearch(name, engines);
    await refresh();
  }

  async function claimDiscovery(discovery: Discovery, name: string) {
    await saveClient(name, {
      profile: defaultProfile(),
      notes: `discovered at ${discovery.address}`,
      addresses: [discovery.address],
      macs: [discovery.mac],
      prefixes: [],
    });
    await removeDiscovery(discovery.mac);
    await refresh();
  }

  async function saveAccess(input: AccessSettings) {
    await putAccess(input);
    await refresh();
  }

  return (
    <>
      <div class="sky" aria-hidden="true">
        <div class="sky-layer far" style={{ "background-image": sky.far }} />
        <div class="sky-layer near" style={{ "background-image": sky.near }} />
      </div>
      <div class="shell">
        <nav class="sidebar">
          <div class="brand">
            <div class="brand-mark">
              <svg width="18" height="18" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M4 12.4 L8 7.2 L12.6 10 L13 3.6"
                  stroke="currentColor"
                  stroke-width="1.2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  opacity="0.7"
                />
                <circle cx="4" cy="12.4" r="1.3" fill="currentColor" />
                <circle cx="8" cy="7.2" r="1.5" fill="currentColor" />
                <circle cx="12.6" cy="10" r="1.1" fill="currentColor" />
                <circle cx="13" cy="3.6" r="1.7" fill="currentColor" />
              </svg>
            </div>
            <div class="brand-text">
              <span class="brand-name">AEGIS</span>
              <span class="brand-sub">sinkhole</span>
            </div>
          </div>
          <div class="nav">
            {NAV_GROUPS.map((group) => (
              <div class="nav-group">
                <span class="nav-label">{group.label}</span>
                {group.items.map((item) => (
                  <button
                    type="button"
                    data-testid={`tab-${item.id}`}
                    class={tab() === item.id ? "nav-item active" : "nav-item"}
                    aria-current={tab() === item.id ? "page" : undefined}
                    title={item.label}
                    onClick={() => setTab(item.id)}
                  >
                    <span class="nav-icon">{item.icon()}</span>
                    <span class="nav-text">{item.label}</span>
                  </button>
                ))}
              </div>
            ))}
          </div>
          <div class="foot">
            {rules().length} custom · {ruleCount()} list rules
          </div>
        </nav>
        <div class="content">
          <header class="topbar">
            <h1 class="page-title">{TITLES[tab()]}</h1>
            <div class="status-pill" data-testid="status">
              <span class="dot" />
              <span class="status-text">
                {upstreams()[0] || "no upstream"}
                {upstreams().length > 1 ? ` +${upstreams().length - 1}` : ""} · {ruleCount()} rules
              </span>
            </div>
          </header>
          <Show when={error()}>
            <p class="error-banner">Could not load the configuration: {error()}</p>
          </Show>
          <Show when={tab() === "constellation"}>
            <main class="screen-bleed" data-testid="screen-constellation">
              <Graph
                profiles={profiles()}
                clients={clients()}
                rules={rules()}
                defaultProfile={defaultProfile()}
                upstreams={upstreams()}
                log={log}
                onSaveClient={saveClient}
                onSaveProfile={saveProfile}
                onSetDefault={makeDefault}
              />
            </main>
          </Show>
          <Show when={tab() === "dashboard"}>
            <main class="screen">
              <Dashboard
                entries={log.entries()}
                threats={threats()}
                windowMinutes={windowMinutes()}
                onSetWindow={setWindowMinutes}
                onOpenLog={() => setTab("log")}
                onFilter={(filter) => {
                  setLogFilter(filter);
                  setTab("log");
                }}
              />
            </main>
          </Show>
          <Show when={tab() === "log"}>
            <main class="screen">
              <QueryLog
                log={log}
                filter={logFilter()}
                onRuleAdded={async () => {
                  await refresh();
                }}
              />
            </main>
          </Show>
          <Show when={tab() === "clients"}>
            <main class="screen">
              <div class="screen-inner">
                <Clients
                  clients={clients()}
                  profiles={profiles()}
                  discoveries={discoveries()}
                  onClaimDiscovery={claimDiscovery}
                  onDismissDiscovery={async (mac) => {
                    await removeDiscovery(mac);
                    await refresh();
                  }}
                  onSave={saveClient}
                  onDelete={deleteClient}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "services"}>
            <main class="screen">
              <div class="screen-inner">
                <BlockedServices
                  services={services()}
                  serviceGroups={serviceGroups()}
                  profileNames={profiles().map((profile) => profile.name)}
                  clients={clients()}
                  defaultProfile={defaultProfile()}
                  schedules={schedules()}
                  windows={windows()}
                  onSave={saveClientServices}
                  onRefreshServices={refreshServiceCatalog}
                  onSaveWindow={addWindow}
                  onDeleteWindow={deleteServiceWindow}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "profiles"}>
            <main class="screen">
              <div class="screen-inner">
                <Profiles
                  profiles={profiles()}
                  defaultProfile={defaultProfile()}
                  safesearch={safesearch()}
                  onSave={saveProfile}
                  onDelete={deleteProfile}
                  onSetDefault={makeDefault}
                  onSaveSafesearch={saveProfileSafesearch}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "rules"}>
            <main class="screen">
              <div class="screen-inner">
                <Rules
                  rules={rules()}
                  schedules={schedules().map((schedule) => schedule.name)}
                  clients={clients().map((client) => client.name)}
                  onCreate={addRule}
                  onUpdate={changeRule}
                  onDelete={deleteRule}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "schedules"}>
            <main class="screen">
              <div class="screen-inner">
                <Schedules
                  schedules={schedules()}
                  rules={rules()}
                  onSave={addSchedule}
                  onDelete={deleteSchedule}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "rewrites"}>
            <main class="screen">
              <div class="screen-inner">
                <Rewrites rewrites={rewrites()} onSave={saveRewrite} onDelete={deleteRewrite} />
              </div>
            </main>
          </Show>
          <Show when={tab() === "sources"}>
            <main class="screen">
              <div class="screen-inner">
                <Sources
                  sources={sources()}
                  catalog={catalog()}
                  onSave={saveSource}
                  onDelete={deleteSource}
                  onReload={refresh}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "upstreams"}>
            <main class="screen">
              <div class="screen-inner">
                <Upstreams
                  upstreams={upstreamRows()}
                  routes={routes()}
                  onSaveUpstream={saveUpstream}
                  onDeleteUpstream={deleteUpstream}
                  onCreateRoute={addRoute}
                  onUpdateRoute={changeRoute}
                  onDeleteRoute={deleteRoute}
                />
              </div>
            </main>
          </Show>
          <Show when={tab() === "settings"}>
            <main class="screen">
              <Settings
                profiles={profiles()}
                defaultProfile={defaultProfile()}
                onSetDefault={makeDefault}
                access={access()}
                onSaveAccess={saveAccess}
                windowMinutes={windowMinutes()}
                onSetWindow={setWindowMinutes}
                live={live()}
                onSetLive={setLive}
              />
            </main>
          </Show>
        </div>
      </div>
    </>
  );
}
