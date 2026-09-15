import { createEffect, createSignal } from "solid-js";
import {
  createRule as postRule,
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  deleteRule as removeRule,
  deleteSource as removeSource,
  getDefaultProfile,
  getStatus,
  listCatalog,
  listClients,
  listProfiles,
  listRules,
  listSources,
  saveClient as putClient,
  saveProfile as putProfile,
  saveSource as putSource,
  setDefaultProfile as putDefaultProfile,
  updateRule as patchRule,
  type CatalogEntry,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
  type Rule,
  type RuleInput,
  type Source,
  type SourceInput,
} from "./api";
import Clients from "./Clients";
import Graph from "./Graph";
import Profiles from "./Profiles";
import Rules from "./Rules";
import Sources from "./Sources";

type Tab = "profiles" | "clients" | "sources" | "rules" | "graph";

export default function App() {
  const [tab, setTab] = createSignal<Tab>("profiles");
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [sources, setSources] = createSignal<Source[]>([]);
  const [catalog, setCatalog] = createSignal<CatalogEntry[]>([]);
  const [rules, setRules] = createSignal<Rule[]>([]);
  const [defaultProfile, setDefaultProfile] = createSignal("");
  const [upstream, setUpstream] = createSignal("");
  const [error, setError] = createSignal<string>();

  async function refresh() {
    const [nextProfiles, nextClients, nextSources, nextCatalog, nextRules, nextDefault, nextStatus] =
      await Promise.all([
        listProfiles(),
        listClients(),
        listSources(),
        listCatalog(),
        listRules(),
        getDefaultProfile(),
        getStatus(),
      ]);
    setProfiles(nextProfiles);
    setClients(nextClients);
    setSources(nextSources);
    setCatalog(nextCatalog);
    setRules(nextRules);
    setDefaultProfile(nextDefault.profile);
    setUpstream(nextStatus.upstream);
  }

  createEffect(
    () => undefined,
    () => {
      void refresh().catch((cause) => setError(String(cause)));
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

  async function makeDefault(name: string) {
    await putDefaultProfile(name);
    await refresh();
  }

  return (
    <main>
      <header>
        <h1>Aegis</h1>
        <nav>
          <button
            type="button"
            data-testid="tab-profiles"
            class={tab() === "profiles" ? "active" : ""}
            onClick={() => setTab("profiles")}
          >
            Profiles
          </button>
          <button
            type="button"
            data-testid="tab-clients"
            class={tab() === "clients" ? "active" : ""}
            onClick={() => setTab("clients")}
          >
            Clients
          </button>
          <button
            type="button"
            data-testid="tab-sources"
            class={tab() === "sources" ? "active" : ""}
            onClick={() => setTab("sources")}
          >
            Sources
          </button>
          <button
            type="button"
            data-testid="tab-rules"
            class={tab() === "rules" ? "active" : ""}
            onClick={() => setTab("rules")}
          >
            Rules
          </button>
          <button
            type="button"
            data-testid="tab-graph"
            class={tab() === "graph" ? "active" : ""}
            onClick={() => setTab("graph")}
          >
            Graph
          </button>
        </nav>
      </header>
      {error() ? <p class="error">Could not load the configuration: {error()}</p> : null}
      {tab() === "profiles" ? (
        <Profiles
          profiles={profiles()}
          defaultProfile={defaultProfile()}
          onSave={saveProfile}
          onDelete={deleteProfile}
          onSetDefault={makeDefault}
        />
      ) : tab() === "clients" ? (
        <Clients
          clients={clients()}
          profiles={profiles()}
          onSave={saveClient}
          onDelete={deleteClient}
        />
      ) : tab() === "sources" ? (
        <Sources
          sources={sources()}
          catalog={catalog()}
          onSave={saveSource}
          onDelete={deleteSource}
        />
      ) : tab() === "rules" ? (
        <Rules rules={rules()} onCreate={addRule} onUpdate={changeRule} onDelete={deleteRule} />
      ) : (
        <Graph
          profiles={profiles()}
          clients={clients()}
          defaultProfile={defaultProfile()}
          upstream={upstream()}
        />
      )}
    </main>
  );
}
