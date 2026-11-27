// Client for the OpenRouter key management API. It needs a management key:
// https://openrouter.ai/docs/guides/overview/auth/management-api-keys
import { ServiceError } from "../../errors.ts";

export type LimitReset = "daily" | "weekly" | "monthly";

export interface ApiKey {
  hash: string;
  name: string;
  label: string; // masked, for example sk-or-v1-0e6...1c96
  disabled: boolean;
  limit: number | null; // USD
  limit_remaining: number | null;
  limit_reset: LimitReset | null;
  usage: number; // USD, all time
  usage_daily: number;
  usage_weekly: number;
  usage_monthly: number;
  created_at: string;
  last_used_at: string | null;
  expires_at: string | null;
}

export interface Credits {
  total_credits: number;
  total_usage: number;
}

export interface CreateKeyInput {
  name: string;
  limit: number;
  limit_reset: LimitReset;
  expires_at?: string; // ISO 8601 UTC with seconds
}

export interface UpdateKeyInput {
  limit?: number;
  limit_reset?: LimitReset;
  disabled?: boolean;
}

export interface OpenRouterKeys {
  getCredits(): Promise<Credits>;
  listKeys(opts?: { includeDisabled?: boolean }): Promise<ApiKey[]>;
  getKey(hash: string): Promise<ApiKey>;
  createKey(input: CreateKeyInput): Promise<{ data: ApiKey; key: string }>;
  updateKey(hash: string, input: UpdateKeyInput): Promise<ApiKey>;
}

export class OpenRouterError extends ServiceError {
  readonly status: number;

  constructor(status: number, message: string) {
    super(`OpenRouter (${status}): ${message}`);
    this.name = "OpenRouterError";
    this.status = status;
  }
}

export class HttpOpenRouterKeys implements OpenRouterKeys {
  readonly #managementKey: string;
  readonly #baseUrl: string;
  readonly #fetch: typeof fetch;

  constructor(managementKey: string, opts: { baseUrl?: string; fetch?: typeof fetch } = {}) {
    this.#managementKey = managementKey;
    this.#baseUrl = opts.baseUrl ?? "https://openrouter.ai/api/v1";
    this.#fetch = opts.fetch ?? fetch;
  }

  async #request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.#fetch(`${this.#baseUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${this.#managementKey}`,
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });

    const text = await res.text();
    let json: { error?: { message?: string } } & Record<string, unknown>;
    try {
      json = text ? JSON.parse(text) : {};
    } catch {
      throw new OpenRouterError(res.status, `not JSON: ${text.slice(0, 200)}`);
    }
    if (!res.ok) throw new OpenRouterError(res.status, json.error?.message ?? res.statusText);
    return json as T;
  }

  async getCredits() {
    return (await this.#request<{ data: Credits }>("GET", "/credits")).data;
  }

  async listKeys(opts: { includeDisabled?: boolean } = {}) {
    const keys: ApiKey[] = [];
    const disabled = opts.includeDisabled ? "&include_disabled=true" : "";
    for (let offset = 0; ; ) {
      const page = (await this.#request<{ data: ApiKey[] }>("GET", `/keys?offset=${offset}${disabled}`)).data;
      keys.push(...page);
      if (page.length === 0 || keys.length >= 1000) return keys;
      offset += page.length;
    }
  }

  async getKey(hash: string) {
    return (await this.#request<{ data: ApiKey }>("GET", `/keys/${encodeURIComponent(hash)}`)).data;
  }

  createKey(input: CreateKeyInput) {
    return this.#request<{ data: ApiKey; key: string }>("POST", "/keys", input);
  }

  async updateKey(hash: string, input: UpdateKeyInput) {
    return (await this.#request<{ data: ApiKey }>("PATCH", `/keys/${encodeURIComponent(hash)}`, input)).data;
  }
}
