import type { JSX } from "@solidjs/web";
import { createEffect, createSignal, For, Show } from "solid-js";
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
  IconSearch,
  IconServices,
  IconShield,
  IconSources,
  IconSystem,
  IconUpstream,
} from "./Icons";
import BlockedServices, { type ServiceScope } from "./BlockedServices";
import Clients from "./Clients";
import Dashboard from "./Dashboard";
import Graph from "./Graph";
import QueryLog from "./QueryLog";
import Profiles from "./Profiles";
import Rewrites from "./Rewrites";
import Rules from "./Rules";
import Schedules from "./Schedules";
import Sources from "./Sources";
import Upstreams from "./Upstreams";
import Settings from "./Settings";
import { createQueryLog } from "./querylog";
import { starField } from "./sky";

type Tab =
  | "dashboard"
  | "constellation"
  | "log"
  | "clients"
  | "profiles"
  | "rules"
  | "schedules"
  | "rewrites"
  | "services"
  | "system";

type SystemView = "upstreams" | "sources" | "settings";

const RAIL: { id: Tab; label: string; icon: () => JSX.Element }[] = [
  { id: "dashboard", label: "Overview", icon: IconDashboard },
  { id: "constellation", label: "Constellation", icon: IconConstellation },
  { id: "log", label: "Query Log", icon: IconLog },
  { id: "clients", label: "Clients", icon: IconClients },
  { id: "profiles", label: "Profiles", icon: IconShield },
  { id: "rules", label: "Rules", icon: IconRules },
  { id: "schedules", label: "Schedules", icon: IconClock },
  { id: "rewrites", label: "Rewrites", icon: IconRewrite },
  { id: "services", label: "Blocked Services", icon: IconServices },
  { id: "system", label: "System", icon: IconSystem },
];

const VIEW_NAMES: Record<Tab, string> = {
  dashboard: "Overview",
  constellation: "Constellation",
  log: "Query Log",
  clients: "Clients",
  profiles: "Profiles",
  rules: "Rules",
  schedules: "Schedules",
  rewrites: "Rewrites",
  services: "Blocked Services",
  system: "System",
};

const SYSTEM_VIEWS: { id: SystemView; label: string }[] = [
  { id: "upstreams", label: "Upstreams & routes" },
  { id: "sources", label: "Block sources" },
  { id: "settings", label: "Settings" },
];

// The sky is seeded, so every load paints the same night.
const sky = starField();

export default function App() {
  const [tab, setTab] = createSignal<Tab>("dashboard");
  const [systemView, setSystemView] = createSignal<SystemView>("upstreams");
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
  const [paletteOpen, setPaletteOpen] = createSignal(false);
  const [paletteQuery, setPaletteQuery] = createSignal("");
  const [paletteIndex, setPaletteIndex] = createSignal(0);

  const log = createQueryLog({ live: live() });

  const paletteItems = () => {
    const items: { label: string; hint: string; keywords: string; run: () => void }[] = RAIL.map((item) => ({
      label: item.label,
      hint: "view",
      keywords: item.label.toLowerCase(),
      run: () => setTab(item.id),
    }));
    items.push({
      label: live() ? "Stop the live query stream" : "Start the live query stream",
      hint: "action",
      keywords: "live stream toggle pause resume",
      run: () => setLive((current) => !current),
    });
    items.push({
      label: "Refresh the services catalog",
      hint: "action",
      keywords: "services catalog refresh",
      run: () => {
        void postRefreshServices()
          .then(() => refresh())
          .catch((cause) => setError(String(cause)));
      },
    });
    const query = paletteQuery().trim().toLowerCase();
    if (!query) {
      return items;
    }
    return items.filter((item) => item.keywords.includes(query) || item.label.toLowerCase().includes(query));
  };

  function openPalette() {
    setPaletteQuery("");
    setPaletteIndex(0);
    setPaletteOpen(true);
  }

  function runPaletteItem(index: number) {
    const item = paletteItems()[index];
    if (!item) {
      return;
    }
    setPaletteOpen(false);
    item.run();
  }

  function onPaletteKey(event: KeyboardEvent) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setPaletteIndex((index) => Math.min(index + 1, paletteItems().length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setPaletteIndex((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      runPaletteItem(paletteIndex());
    } else if (event.key === "Escape") {
      setPaletteOpen(false);
    }
  }

  createEffect(
    () => undefined,
    () => {
      const onKey = (event: KeyboardEvent) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
          event.preventDefault();
          if (paletteOpen()) {
            setPaletteOpen(false);
          } else {
            openPalette();
          }
        }
      };
      window.addEventListener("keydown", onKey);
      return () => window.removeEventListener("keydown", onKey);
    },
  );

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
      <div class="app">
        <nav class="rail" aria-label="sections">
          <div class="rail-mark" aria-hidden="true">
            <svg width="20" height="20" viewBox="0 0 16 16" fill="none">
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
          <For each={RAIL}>
            {(item) => (
              <button
                type="button"
                data-testid={`tab-${item.id}`}
                class={tab() === item.id ? "rail-item active" : "rail-item"}
                aria-current={tab() === item.id ? "page" : undefined}
                title={item.label}
                aria-label={item.label}
                onClick={() => setTab(item.id)}
              >
                {item.icon()}
              </button>
            )}
          </For>
        </nav>

        <header class="cmdbar">
          <span class="view-name">{VIEW_NAMES[tab()]}</span>
          <button type="button" class="palette-trigger" data-testid="palette-trigger" onClick={() => openPalette()}>
            <IconSearch />
            <span>Jump to a view or run an action</span>
            <kbd>⌘K</kbd>
          </button>
          <div class="cmdbar-status">
            <span class={`live-chip${live() ? "" : " off"}`} data-testid="live-chip">
              <span class="live-dot" />
              {live() ? "live" : "paused"}
            </span>
            <span class="status-pill" data-testid="status">
              <span class="dot" />
              <span class="status-text">
                {upstreams()[0] || "no upstream"}
                {upstreams().length > 1 ? ` +${upstreams().length - 1}` : ""} · {ruleCount()} rules
              </span>
            </span>
          </div>
        </header>

        <main class="canvas">
          <Show when={error()}>
            <p class="alert-line error-line" role="alert">
              Could not load the configuration: {error()}
            </p>
          </Show>
          <Show when={tab() === "dashboard"}>
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
          </Show>
          <Show when={tab() === "constellation"}>
            <div class="view" data-testid="screen-constellation">
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
            </div>
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
          <Show when={tab() === "clients"}>
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
          </Show>
          <Show when={tab() === "profiles"}>
            <Profiles
              profiles={profiles()}
              defaultProfile={defaultProfile()}
              safesearch={safesearch()}
              onSave={saveProfile}
              onDelete={deleteProfile}
              onSetDefault={makeDefault}
              onSaveSafesearch={saveProfileSafesearch}
            />
          </Show>
          <Show when={tab() === "rules"}>
            <Rules
              rules={rules()}
              schedules={schedules().map((schedule) => schedule.name)}
              clients={clients().map((client) => client.name)}
              onCreate={addRule}
              onUpdate={changeRule}
              onDelete={deleteRule}
            />
          </Show>
          <Show when={tab() === "schedules"}>
            <Schedules
              schedules={schedules()}
              rules={rules()}
              onSave={addSchedule}
              onDelete={deleteSchedule}
            />
          </Show>
          <Show when={tab() === "rewrites"}>
            <Rewrites rewrites={rewrites()} onSave={saveRewrite} onDelete={deleteRewrite} />
          </Show>
          <Show when={tab() === "services"}>
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
          </Show>
          <Show when={tab() === "system"}>
            <div class="view">
              <div class="system-subnav" role="tablist" aria-label="system views">
                <For each={SYSTEM_VIEWS}>
                  {(view) => (
                    <button
                      type="button"
                      data-testid={`tab-${view.id}`}
                      class={systemView() === view.id ? "btn-mini active" : "btn-mini"}
                      aria-current={systemView() === view.id ? "page" : undefined}
                      onClick={() => setSystemView(view.id)}
                    >
                      {view.id === "upstreams" ? <IconUpstream /> : view.id === "sources" ? <IconSources /> : <IconGear />}
                      {view.label}
                    </button>
                  )}
                </For>
              </div>
              <Show when={systemView() === "upstreams"}>
                <Upstreams
                  upstreams={upstreamRows()}
                  routes={routes()}
                  onSaveUpstream={saveUpstream}
                  onDeleteUpstream={deleteUpstream}
                  onCreateRoute={addRoute}
                  onUpdateRoute={changeRoute}
                  onDeleteRoute={deleteRoute}
                />
              </Show>
              <Show when={systemView() === "sources"}>
                <Sources
                  sources={sources()}
                  catalog={catalog()}
                  onSave={saveSource}
                  onDelete={deleteSource}
                  onReload={refresh}
                />
              </Show>
              <Show when={systemView() === "settings"}>
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
            </div>
          </Show>
        </main>

        <footer class="strip">
          <span>{rules().length} custom rules</span>
          <span class="sep">·</span>
          <span>{ruleCount()} list rules</span>
          <span class="sep">·</span>
          <span>{clients().length} clients</span>
          <span class="sep">·</span>
          <span>{profiles().length} profiles</span>
          <span class="sep">·</span>
          <span>{upstreams()[0] || "no upstream"}</span>
          <span class="sep">·</span>
          <span>aegis console</span>
        </footer>
      </div>

      <Show when={paletteOpen()}>
        <div class="palette-scrim" onClick={() => setPaletteOpen(false)} />
        <div class="palette" role="dialog" aria-modal="true" aria-label="command palette" data-testid="palette">
          <input
            data-testid="palette-input"
            placeholder="Type a view or action…"
            value={paletteQuery()}
            ref={(element) => setTimeout(() => element.focus(), 0)}
            onInput={(event) => {
              setPaletteQuery(event.currentTarget.value);
              setPaletteIndex(0);
            }}
            onKeyDown={onPaletteKey}
          />
          <ul class="palette-list">
            <For each={paletteItems()}>
              {(item, index) => (
                <li>
                  <button
                    type="button"
                    class={paletteIndex() === index() ? "active" : undefined}
                    onMouseEnter={() => setPaletteIndex(index())}
                    onClick={() => runPaletteItem(index())}
                  >
                    <span class="p-label">{item.label}</span>
                    <span class="p-hint">{item.hint}</span>
                  </button>
                </li>
              )}
            </For>
            <Show when={paletteItems().length === 0}>
              <li class="palette-empty">nothing matches</li>
            </Show>
          </ul>
        </div>
      </Show>
    </>
  );
}
