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
};

// Absent fields keep what is stored, so a one-field body toggles or repoints a
// source without resending the rest.
export type SourceInput = {
  url?: string;
  format?: string;
  enabled?: boolean;
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
  notes?: string;
  created?: string;
};

// A rule has no inherited fields, so every field the body names is replaced and
// the rest survive.
export type RuleInput = {
  domain?: string;
  kind?: string;
  action?: string;
  notes?: string;
};

export type Status = {
  upstream: string;
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
