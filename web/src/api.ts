export type Profile = {
  name: string;
  extends?: string;
  mode?: string;
  custom?: string;
};

export type Client = {
  name: string;
  profile: string;
  notes: string;
  addresses: string[];
  prefixes: string[];
};

// A blank field means inherit or none, so only the fields that were set travel
// on the wire.
export type ProfileInput = {
  extends?: string;
  mode?: string;
  custom?: string;
};

export type ClientInput = {
  profile: string;
  notes: string;
  addresses: string[];
  prefixes: string[];
};

export type Source = {
  name: string;
  url: string;
  format: string;
  enabled: boolean;
  last_fetch?: string;
  last_error?: string;
  rule_count: number;
  skipped: number;
  failures: number;
  refresh_seconds: number;
};

// Absent fields keep what is stored, so a one-field body toggles or repoints a
// source without resending the rest.
export type SourceInput = {
  url?: string;
  format?: string;
  enabled?: boolean;
  refresh_seconds?: number;
};

export type CatalogEntry = {
  name: string;
  url: string;
  format: string;
};

export type Rule = {
  id: number;
  domain: string;
  kind: string;
  action: string;
  schedule?: string;
  client?: string;
  notes?: string;
  created?: string;
};

// A rule has no inherited fields, so every field the body names is replaced and
// the rest survive. A schedule keeps the rule active only while its windows
// cover the query minute, and a client scopes it to one identity.
export type RuleInput = {
  domain?: string;
  kind?: string;
  action?: string;
  schedule?: string;
  client?: string;
  notes?: string;
};

export type ScheduleWindow = {
  days: number[];
  start: string;
  end: string;
};

export type Schedule = {
  name: string;
  priority: number;
  windows: ScheduleWindow[];
};

export type ScheduleInput = {
  priority: number;
  windows: ScheduleWindow[];
};

export type Status = {
  upstreams: string[];
  rules?: number;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!response.ok) {
    throw new Error(await errorMessage(response));
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: string };
    return body.error ?? `HTTP ${response.status}`;
  } catch {
    return `HTTP ${response.status}`;
  }
}

export function getStatus(): Promise<Status> {
  return request<Status>("/api/v1/status");
}

export function listProfiles(): Promise<Profile[]> {
  return request<Profile[]>("/api/v1/profiles");
}

export function saveProfile(name: string, input: ProfileInput): Promise<Profile> {
  const body: Record<string, string> = {};
  if (input.extends) body.extends = input.extends;
  if (input.mode) body.mode = input.mode;
  if (input.custom) body.custom = input.custom;
  return request<Profile>(`/api/v1/profiles/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export function deleteProfile(name: string): Promise<void> {
  return request<void>(`/api/v1/profiles/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export function getDefaultProfile(): Promise<{ profile: string }> {
  return request<{ profile: string }>("/api/v1/default-profile");
}

export function setDefaultProfile(profile: string): Promise<{ profile: string }> {
  return request<{ profile: string }>("/api/v1/default-profile", {
    method: "PUT",
    body: JSON.stringify({ profile }),
  });
}

export function listClients(): Promise<Client[]> {
  return request<Client[]>("/api/v1/clients");
}

export function saveClient(name: string, input: ClientInput): Promise<Client> {
  return request<Client>(`/api/v1/clients/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteClient(name: string): Promise<void> {
  return request<void>(`/api/v1/clients/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export function listSources(): Promise<Source[]> {
  return request<Source[]>("/api/v1/sources");
}

export function saveSource(name: string, input: SourceInput): Promise<Source> {
  const body: Record<string, unknown> = {};
  if (input.url !== undefined) body.url = input.url;
  if (input.format !== undefined) body.format = input.format;
  if (input.enabled !== undefined) body.enabled = input.enabled;
  return request<Source>(`/api/v1/sources/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export function deleteSource(name: string): Promise<void> {
  return request<void>(`/api/v1/sources/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export function listCatalog(): Promise<CatalogEntry[]> {
  return request<CatalogEntry[]>("/api/v1/sources/catalog");
}

export type SourcePreview = {
  name: string;
  added: string[];
  removed: string[];
  notModified: boolean;
};

// previewSource fetches the remote list and reports the domains a refresh
// would add and remove, without applying anything.
export function previewSource(name: string): Promise<SourcePreview> {
  return request<SourcePreview>(`/api/v1/sources/${encodeURIComponent(name)}/preview`);
}

export function refreshSource(name: string): Promise<void> {
  return request<void>(`/api/v1/sources/${encodeURIComponent(name)}/refresh`, { method: "POST" });
}

export function listRules(): Promise<Rule[]> {
  return request<Rule[]>("/api/v1/rules");
}

export function createRule(input: RuleInput): Promise<Rule> {
  return request<Rule>("/api/v1/rules", { method: "POST", body: JSON.stringify(input) });
}

export function updateRule(id: number, input: RuleInput): Promise<Rule> {
  return request<Rule>(`/api/v1/rules/${id}`, { method: "PUT", body: JSON.stringify(input) });
}

export function deleteRule(id: number): Promise<void> {
  return request<void>(`/api/v1/rules/${id}`, { method: "DELETE" });
}

export function listSchedules(): Promise<Schedule[]> {
  return request<Schedule[]>("/api/v1/schedules");
}

export function saveSchedule(name: string, input: ScheduleInput): Promise<Schedule> {
  return request<Schedule>(`/api/v1/schedules/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteSchedule(name: string): Promise<void> {
  return request<void>(`/api/v1/schedules/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export type Rewrite = {
  pattern: string;
  target: string;
};

export function listRewrites(): Promise<Rewrite[]> {
  return request<Rewrite[]>("/api/v1/rewrites");
}

export function saveRewrite(pattern: string, target: string): Promise<Rewrite> {
  return request<Rewrite>(`/api/v1/rewrites/${encodeURIComponent(pattern)}`, {
    method: "PUT",
    body: JSON.stringify({ target }),
  });
}

export function deleteRewrite(pattern: string): Promise<void> {
  return request<void>(`/api/v1/rewrites/${encodeURIComponent(pattern)}`, { method: "DELETE" });
}

export type Upstream = {
  name: string;
  url: string;
  enabled: boolean;
  backup: boolean;
  latency_ms: number;
  failures: number;
  down: boolean;
};

export type UpstreamInput = {
  url: string;
  enabled: boolean;
  backup: boolean;
};

export function listUpstreams(): Promise<Upstream[]> {
  return request<Upstream[]>("/api/v1/upstreams");
}

export function saveUpstream(name: string, input: UpstreamInput): Promise<Upstream> {
  return request<Upstream>(`/api/v1/upstreams/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteUpstream(name: string): Promise<void> {
  return request<void>(`/api/v1/upstreams/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export type Route = {
  id: number;
  domain: string;
  client: string;
  upstream: string;
};

// A blank domain matches every name and a blank client matches every client,
// so the router fills the gaps a rule leaves open.
export type RouteInput = {
  domain: string;
  client: string;
  upstream: string;
};

export function listRoutes(): Promise<Route[]> {
  return request<Route[]>("/api/v1/routes");
}

export function createRoute(input: RouteInput): Promise<Route> {
  return request<Route>("/api/v1/routes", { method: "POST", body: JSON.stringify(input) });
}

export function updateRoute(id: number, input: RouteInput): Promise<Route> {
  return request<Route>(`/api/v1/routes/${id}`, { method: "PUT", body: JSON.stringify(input) });
}

export function deleteRoute(id: number): Promise<void> {
  return request<void>(`/api/v1/routes/${id}`, { method: "DELETE" });
}

export type QueryEntry = {
  time: string;
  client: string;
  name: string;
  type: string;
  verdict: string;
  rule?: string;
};

export type QueryFilterInput = {
  client?: string;
  name?: string;
  verdict?: string;
  limit?: number;
};

export function listQueries(filter: QueryFilterInput = {}): Promise<{ queries: QueryEntry[] }> {
  const params = new URLSearchParams();
  if (filter.client) params.set("client", filter.client);
  if (filter.name) params.set("name", filter.name);
  if (filter.verdict) params.set("verdict", filter.verdict);
  if (filter.limit) params.set("limit", String(filter.limit));
  const query = params.toString();
  return request<{ queries: QueryEntry[] }>(`/api/v1/queries${query ? `?${query}` : ""}`);
}

export type Decision = {
  time: string;
  address: string;
  name: string;
  type: string;
  action: string;
  rule?: { id: string; source: string; pattern: string };
};

// streamQueries subscribes to the live decision stream and returns the
// unsubscribe function. The browser's EventSource reconnects on its own, which
// a long-lived dashboard wants.
export function streamQueries(onEvent: (decision: Decision) => void): () => void {
  const source = new EventSource("/api/v1/stream/queries");
  source.onmessage = (message) => {
    try {
      onEvent(JSON.parse(message.data) as Decision);
    } catch {
      return;
    }
  };
  return () => source.close();
}
