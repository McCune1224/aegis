import { createEffect, createSignal } from "solid-js";
import {
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  listClients,
  listProfiles,
  saveClient as putClient,
  saveProfile as putProfile,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
} from "./api";
import Clients from "./Clients";
import Profiles from "./Profiles";

type Tab = "profiles" | "clients";

export default function App() {
  const [tab, setTab] = createSignal<Tab>("profiles");
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [error, setError] = createSignal<string>();

  async function refresh() {
    const [nextProfiles, nextClients] = await Promise.all([listProfiles(), listClients()]);
    setProfiles(nextProfiles);
    setClients(nextClients);
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
        </nav>
      </header>
      {error() ? <p class="error">Could not load the configuration: {error()}</p> : null}
      {tab() === "profiles" ? (
        <Profiles profiles={profiles()} onSave={saveProfile} onDelete={deleteProfile} />
      ) : (
        <Clients
          clients={clients()}
          profiles={profiles()}
          onSave={saveClient}
          onDelete={deleteClient}
        />
      )}
    </main>
  );
}
