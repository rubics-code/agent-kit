// Client for the Infisical API, with a machine identity (Universal Auth).
import { ServiceError } from "../../errors.ts";
import type { SecretPlace, SecretStore } from "../../secret-store.ts";

export class InfisicalError extends ServiceError {
  readonly status: number;
  constructor(status: number, message: string) {
    super(`Infisical ${status}: ${message}`);
    this.name = "InfisicalError";
    this.status = status;
  }
}

export interface IdentityAuth {
  clientId: string;
  clientSecret: string;
}

// A static token, for example a placeholder that the Agent Vault proxy replaces.
export interface TokenAuth {
  token: string;
}

export class HttpInfisical implements SecretStore {
  readonly #auth: IdentityAuth | TokenAuth;
  readonly #baseUrl: string;
  readonly #projectId: string;
  readonly #env: (path: string) => string;
  readonly #fetch: typeof fetch;
  #token: { value: string; until: number } | undefined;

  constructor(
    auth: IdentityAuth | TokenAuth,
    // One environment, or one for each folder path.
    opts: { baseUrl: string; projectId: string; env: string | ((path: string) => string); fetch?: typeof fetch },
  ) {
    this.#auth = auth;
    this.#baseUrl = opts.baseUrl.replace(/\/$/, "");
    this.#projectId = opts.projectId;
    const env = opts.env;
    this.#env = typeof env === "string" ? () => env : env;
    this.#fetch = opts.fetch ?? fetch;
  }

  async #login(): Promise<string> {
    if ("token" in this.#auth) return this.#auth.token;
    if (this.#token && Date.now() < this.#token.until) return this.#token.value;
    const res = await this.#fetch(`${this.#baseUrl}/api/v1/auth/universal-auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ clientId: this.#auth.clientId, clientSecret: this.#auth.clientSecret }),
    });
    const json = (await res.json().catch(() => ({}))) as { accessToken?: string; expiresIn?: number; message?: string };
    if (!res.ok || !json.accessToken) {
      throw new InfisicalError(res.status, `login failed: ${json.message ?? res.statusText}`);
    }
    // Renew one minute early.
    this.#token = { value: json.accessToken, until: Date.now() + ((json.expiresIn ?? 600) - 60) * 1000 };
    return json.accessToken;
  }

  async #request<T>(method: string, path: string, body?: unknown, retry = true): Promise<T> {
    const token = await this.#login();
    const res = await this.#fetch(`${this.#baseUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (res.status === 401 && retry && !("token" in this.#auth)) {
      this.#token = undefined;
      return this.#request(method, path, body, false);
    }
    const text = await res.text();
    let json: unknown;
    try {
      json = text ? JSON.parse(text) : {};
    } catch {
      throw new InfisicalError(res.status, `not JSON: ${text.slice(0, 200)}`);
    }
    if (!res.ok) {
      throw new InfisicalError(res.status, (json as { message?: string }).message ?? res.statusText);
    }
    return json as T;
  }

  #q(path: string, params: Record<string, string>) {
    return new URLSearchParams({ workspaceId: this.#projectId, environment: this.#env(path), ...params }).toString();
  }

  // Bootstrap only: the agent's own keys. Never reachable from a tool.
  async readAll(path: string): Promise<Record<string, string>> {
    const { secrets } = await this.#request<{ secrets: { secretKey: string; secretValue: string }[] }>(
      "GET",
      `/api/v3/secrets/raw?${this.#q(path, { secretPath: path })}`,
    );
    return Object.fromEntries(secrets.map((s) => [s.secretKey, s.secretValue]));
  }

  async listFolders(path: string) {
    return this.#folders(path, path);
  }

  // The folders in `path`, in the environment of `envPath`. To check that
  // /agents exists, list / in the environment of /agents.
  async #folders(path: string, envPath: string) {
    const { folders } = await this.#request<{ folders: { name: string }[] }>(
      "GET",
      `/api/v1/folders?${this.#q(envPath, { path })}`,
    );
    return folders.map((f) => f.name).sort();
  }

  async listSecretNames(path: string) {
    const { secrets } = await this.#request<{ secrets: { secretKey: string }[] }>(
      "GET",
      `/api/v3/secrets/raw?${this.#q(path, { secretPath: path, viewSecretValue: "false" })}`,
    );
    return secrets.map((s) => s.secretKey).sort();
  }

  async #folderExists(path: string) {
    if (path === "/") return true;
    const parent = path.slice(0, path.lastIndexOf("/")) || "/";
    const name = path.slice(path.lastIndexOf("/") + 1);
    try {
      return (await this.#folders(parent, path)).includes(name);
    } catch (err) {
      if (err instanceof InfisicalError && err.status === 404) return false;
      throw err;
    }
  }

  async check(place: SecretPlace) {
    const folderExists = await this.#folderExists(place.path);
    const exists = folderExists && (await this.listSecretNames(place.path)).includes(place.name);
    return { folderExists, exists };
  }

  async put(place: SecretPlace, value: string) {
    let at = "";
    for (const part of place.path.split("/").filter(Boolean)) {
      const parent = at || "/";
      at = `${at}/${part}`;
      if (!(await this.#folderExists(at))) {
        await this.#request("POST", "/api/v1/folders", {
          workspaceId: this.#projectId,
          environment: this.#env(at),
          name: part,
          path: parent,
        });
      }
    }
    const body = { workspaceId: this.#projectId, environment: this.#env(place.path), secretPath: place.path, secretValue: value, type: "shared" };
    const url = `/api/v3/secrets/raw/${encodeURIComponent(place.name)}`;
    const exists = (await this.listSecretNames(place.path)).includes(place.name);
    await this.#request(exists ? "PATCH" : "POST", url, body);
  }
}
