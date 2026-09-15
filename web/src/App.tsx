import { createEffect, createSignal, Show } from "solid-js";
import {
  ApiError,
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  listClients,
  listProfiles,
  login,
  logout,
  saveClient as putClient,
  saveProfile as putProfile,
  session,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
} from "./api";
import Clients from "./Clients";
import Login from "./Login";
import Profiles from "./Profiles";

type Tab = "profiles" | "clients";

export default function App() {
  const [tab, setTab] = createSignal<Tab>("profiles");
  const [ready, setReady] = createSignal(false);
  const [authenticated, setAuthenticated] = createSignal(false);
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
      void (async () => {
        try {
          const status = await session();
          setAuthenticated(status.authenticated);
          if (status.authenticated) {
            await refresh();
          }
        } catch (cause) {
          setError(String(cause));
        } finally {
          setReady(true);
        }
      })();
    },
  );

  async function signIn(password: string) {
    await login(password);
    setError(undefined);
    setAuthenticated(true);
    await refresh();
  }

  async function signOut() {
    await logout();
    setAuthenticated(false);
    setProfiles([]);
    setClients([]);
  }

  // A session can expire between screens, so every mutation that comes back 401
  // drops the app to the login form instead of showing an error it cannot fix.
  async function guarded(action: () => Promise<void>) {
    try {
      await action();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        setAuthenticated(false);
      }
      throw cause;
    }
  }

  async function saveProfile(name: string, input: ProfileInput) {
    await guarded(async () => {
      await putProfile(name, input);
      await refresh();
    });
  }

  async function saveClient(name: string, input: ClientInput) {
    await guarded(async () => {
      await putClient(name, input);
      await refresh();
    });
  }

  async function deleteProfile(name: string) {
    await guarded(async () => {
      await removeProfile(name);
      await refresh();
    });
  }

  async function deleteClient(name: string) {
    await guarded(async () => {
      await removeClient(name);
      await refresh();
    });
  }

  return (
    <main>
      <header>
        <h1>Aegis</h1>
        <Show when={authenticated()}>
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
            <button type="button" data-testid="sign-out" onClick={() => void signOut()}>
              Sign out
            </button>
          </nav>
        </Show>
      </header>
      {error() ? <p class="error">Could not load the configuration: {error()}</p> : null}
      <Show when={ready()} fallback={<p class="muted">Loading.</p>}>
        <Show when={authenticated()} fallback={<Login onSignIn={signIn} />}>
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
        </Show>
      </Show>
    </main>
  );
}
