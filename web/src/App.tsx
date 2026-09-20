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
  listSchedules,
  listSafesearch,
  listServices,
  listSources,
  listUpstreams,
  refreshServices as postRefreshServices,
  saveClient as putClient,
  saveProfile as putProfile,
  saveProfileSafesearch as putProfileSafesearch,
  saveProfileServices as putProfileServices,
  saveRewrite as putRewrite,
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
  type SafesearchEngine,
  type Rewrite,
  type Route,
  type RouteInput,
  type Rule,
  type RuleInput,
  type Schedule,
  type ScheduleInput,
  type Source,
  type SourceInput,
  type Upstream,
  type UpstreamInput,
  type AccessSettings,
} from "./api";
import { IconClients, IconClock, IconDashboard, IconGear, IconGraph, IconLog, IconRewrite, IconRules, IconShield, IconSources, IconUpstream } from "./Icons";
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

type Tab = "dashboard" | "log" | "profiles" | "clients" | "sources" | "rules" | "schedules" | "rewrites" | "upstreams" | "settings" | "graph";

type NavItem = { id: Tab; label: string; icon: () => JSX.Element };

const NAV: NavItem[] = [
  { id: "dashboard", label: "Dashboard", icon: IconDashboard },
  { id: "log", label: "Query Log", icon: IconLog },
  { id: "graph", label: "Constellation", icon: IconGraph },
  { id: "clients", label: "Clients", icon: IconClients },
  { id: "profiles", label: "Profiles", icon: IconShield },
  { id: "rules", label: "Rules", icon: IconRules },
  { id: "schedules", label: "Schedules", icon: IconClock },
  { id: "rewrites", label: "Rewrites", icon: IconRewrite },
  { id: "sources", label: "Sources", icon: IconSources },
  { id: "upstreams", label: "Upstreams", icon: IconUpstream },
  { id: "settings", label: "Settings", icon: IconGear },
];

const TITLES: Record<Tab, string> = {
  dashboard: "Dashboard",
  log: "Query Log",
  graph: "Constellation",
  clients: "Clients",
  profiles: "Profiles",
  rules: "Rules",
  schedules: "Schedules",
  rewrites: "Rewrites",
  sources: "Sources",
  upstreams: "Upstreams",
  settings: "Settings",
};

export default function App() {
  const [tab, setTab] = createSignal<Tab>("dashboard");
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [sources, setSources] = createSignal<Source[]>([]);
  const [catalog, setCatalog] = createSignal<CatalogEntry[]>([]);
  const [services, setServices] = createSignal<BlockedService[]>([]);
  const [serviceGroups, setServiceGroups] = createSignal<string[]>([]);
  const [safesearch, setSafesearch] = createSignal<SafesearchEngine[]>([]);
  const [rules, setRules] = createSignal<Rule[]>([]);
  const [schedules, setSchedules] = createSignal<Schedule[]>([]);
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
    const [nextProfiles, nextClients, nextSources, nextCatalog, nextRules, nextSchedules, nextRewrites, nextUpstreams, nextRoutes, nextAccess, nextDefault, nextStatus, nextServices, nextSafesearch] =
      await Promise.all([
        listProfiles(),
        listClients(),
        listSources(),
        listCatalog(),
        listRules(),
        listSchedules(),
        listRewrites(),
        listUpstreams(),
        listRoutes(),
        getAccess(),
        getDefaultProfile(),
        getStatus(),
        listServices(),
        listSafesearch(),
      ]);
    setProfiles(nextProfiles);
    setClients(nextClients);
    setSources(nextSources);
    setCatalog(nextCatalog);
    setRules(nextRules);
    setSchedules(nextSchedules);
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

  async function saveProfileServices(name: string, serviceIDs: string[]) {
    await putProfileServices(name, serviceIDs);
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

  async function saveAccess(input: AccessSettings) {
    await putAccess(input);
    await refresh();
  }

  return (
    <>
      <div class="sky" />
      <div class="shell">
        <aside class="sidebar">
          <div class="brand">
            <strong>Aegis</strong>
            <span>sinkhole</span>
          </div>
          <nav class="nav">
            {NAV.map((item) => (
              <button
                type="button"
                data-testid={`tab-${item.id}`}
                class={tab() === item.id ? "nav-item active" : "nav-item"}
                onClick={() => setTab(item.id)}
              >
                {item.icon()}
                {item.label}
              </button>
            ))}
          </nav>
          <div class="foot">
            {rules().length} custom · {ruleCount()} list rules
          </div>
        </aside>
        <div class="content">
          <header class="topbar">
            <h1 class="page-title">{TITLES[tab()]}</h1>
            <div class="pill" data-testid="status">
              <span class="dot" />
              {upstreams()[0] || "no upstream"}
              {upstreams().length > 1 ? ` +${upstreams().length - 1}` : ""} · {ruleCount()} rules
            </div>
          </header>
          <Show when={error()}>
            <p class="error-banner">Could not load the configuration: {error()}</p>
          </Show>
          <main class="screen">
            <Show when={tab() === "dashboard"}>
              <Dashboard
                entries={log.entries()}
                windowMinutes={windowMinutes()}
                onSetWindow={setWindowMinutes}
                onOpenLog={() => setTab("log")}
                onFilter={(filter) => {
                  setLogFilter(filter);
                  setTab("log");
                }}
              />
            </Show>
            <Show when={tab() === "log"}>
              <QueryLog
                log={log}
                filter={logFilter()}
                onRuleAdded={async () => {
                  await refresh();
                }}
              />
            </Show>
            <Show when={tab() === "graph"}>
              <Graph
                profiles={profiles()}
                clients={clients()}
                defaultProfile={defaultProfile()}
                upstreams={upstreams()}
                log={log}
                onSaveClient={saveClient}
                onSetDefault={makeDefault}
              />
            </Show>
            <Show when={tab() === "clients"}>
              <div class="screen-inner">
                <Clients
                  clients={clients()}
                  profiles={profiles()}
                  onSave={saveClient}
                  onDelete={deleteClient}
                />
              </div>
            </Show>
            <Show when={tab() === "profiles"}>
              <div class="screen-inner">
                <Profiles
                  profiles={profiles()}
                  defaultProfile={defaultProfile()}
                  services={services()}
                  serviceGroups={serviceGroups()}
                  safesearch={safesearch()}
                  onSave={saveProfile}
                  onDelete={deleteProfile}
                  onSetDefault={makeDefault}
                  onSaveServices={saveProfileServices}
                  onRefreshServices={refreshServiceCatalog}
                  onSaveSafesearch={saveProfileSafesearch}
                />
              </div>
            </Show>
            <Show when={tab() === "rules"}>
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
            </Show>
            <Show when={tab() === "schedules"}>
              <div class="screen-inner">
                <Schedules
                  schedules={schedules()}
                  rules={rules()}
                  onSave={addSchedule}
                  onDelete={deleteSchedule}
                />
              </div>
            </Show>
            <Show when={tab() === "rewrites"}>
              <div class="screen-inner">
                <Rewrites rewrites={rewrites()} onSave={saveRewrite} onDelete={deleteRewrite} />
              </div>
            </Show>
            <Show when={tab() === "sources"}>
              <div class="screen-inner">
                <Sources
                  sources={sources()}
                  catalog={catalog()}
                  onSave={saveSource}
                  onDelete={deleteSource}
                  onReload={refresh}
                />
              </div>
            </Show>
            <Show when={tab() === "upstreams"}>
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
            </Show>
            <Show when={tab() === "settings"}>
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
            </Show>
          </main>
        </div>
      </div>
    </>
  );
}
