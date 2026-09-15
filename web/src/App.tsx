import { createEffect, createSignal } from "solid-js";
import {
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  getDefaultProfile,
  getStatus,
  listClients,
  listProfiles,
  saveClient as putClient,
  saveProfile as putProfile,
  setDefaultProfile as putDefaultProfile,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
} from "./api";
import Clients from "./Clients";
import Graph from "./Graph";
import Profiles from "./Profiles";

type Tab = "profiles" | "clients" | "graph";

export default function App() {
  const [tab, setTab] = createSignal<Tab>("profiles");
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [defaultProfile, setDefaultProfile] = createSignal("");
  const [upstream, setUpstream] = createSignal("");
  const [error, setError] = createSignal<string>();

  async function refresh() {
    const [nextProfiles, nextClients, nextDefault, nextStatus] = await Promise.all([
      listProfiles(),
      listClients(),
      getDefaultProfile(),
      getStatus(),
    ]);
    setProfiles(nextProfiles);
    setClients(nextClients);
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
