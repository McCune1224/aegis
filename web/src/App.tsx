import { createEffect, createSignal, For } from "solid-js";

type Profile = {
  name: string;
  mode?: string;
};

export default function App() {
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [error, setError] = createSignal<string>();

  createEffect(
    () => undefined,
    () => {
      void (async () => {
        try {
          const response = await fetch("/api/v1/profiles");
          if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
          }
          setProfiles(await response.json());
        } catch (cause) {
          setError(String(cause));
        }
      })();
    },
  );

  return (
    <main>
      <h1>Aegis</h1>
      {error() ? <p class="error">Could not load profiles: {error()}</p> : null}
      <ul>
        <For each={profiles()}>
          {(profile) => (
            <li>
              <strong>{profile.name}</strong>
              <span>{profile.mode ?? "inherited"}</span>
            </li>
          )}
        </For>
      </ul>
    </main>
  );
}
