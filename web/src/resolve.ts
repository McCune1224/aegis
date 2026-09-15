import type { Profile } from "./api";

// effectiveMode walks the extends chain the way the engine does at compile time,
// so the screens can say where a client's policy actually came from instead of
// showing a bare inherited.
export function effectiveMode(profiles: Profile[], name: string): string {
  const byName = new Map(profiles.map((profile) => [profile.name, profile]));
  const seen = new Set<string>();

  let current = byName.get(name);
  while (current && !seen.has(current.name)) {
    seen.add(current.name);
    if (current.mode) {
      return current.name === name ? current.mode : `${current.mode} from ${current.name}`;
    }
    current = current.extends ? byName.get(current.extends) : undefined;
  }
  return "nxdomain";
}
