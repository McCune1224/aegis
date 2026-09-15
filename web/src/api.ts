export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

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

export type Session = {
  authenticated: boolean;
  csrf?: string;
};

// The CSRF token lives in memory only, so a reload asks the session endpoint for
// a fresh copy before it mutates anything.
let csrfToken = "";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  if (csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) {
    throw new ApiError(response.status, await errorMessage(response));
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

export async function session(): Promise<Session> {
  const status = await request<Session>("/api/v1/session");
  csrfToken = status.csrf ?? "";
  return status;
}

export async function login(password: string): Promise<void> {
  const result = await request<{ csrf: string }>("/api/v1/session", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
  csrfToken = result.csrf;
}

export async function logout(): Promise<void> {
  await request<void>("/api/v1/session", { method: "DELETE" });
  csrfToken = "";
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
