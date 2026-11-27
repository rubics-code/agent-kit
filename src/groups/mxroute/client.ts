// Client for the MXroute REST API. Spec: https://api.mxroute.com/openapi.yaml
import { ServiceError } from "../../errors.ts";

export interface EmailAccount {
  username: string;
  email: string;
  quota: number; // MB, 0 = unlimited
  usage: number; // MB
  limit: number; // sends per day
  sent: number;
  suspended: boolean;
}

export interface Forwarder {
  alias: string;
  email: string;
  destinations: string[];
}

export interface MxrouteDnsRecord {
  type: string;
  name: string;
  value: string;
}

export interface DnsInfo {
  mx_records: { priority: number; hostname: string; description?: string }[];
  spf: MxrouteDnsRecord;
  dkim: MxrouteDnsRecord | null;
  verification: (MxrouteDnsRecord & { description?: string }) | null;
}

export interface VerificationKey {
  key: string;
  record: MxrouteDnsRecord;
}

export interface Mxroute {
  listDomains(): Promise<string[]>;
  getDnsInfo(domain: string): Promise<DnsInfo>;
  getVerificationKey(): Promise<VerificationKey>;
  listEmailAccounts(domain: string): Promise<EmailAccount[]>;
  listForwarders(domain: string): Promise<Forwarder[]>;
  addDomain(domain: string): Promise<void>;
  createEmailAccount(
    domain: string,
    input: { username: string; password: string; quota: number; limit: number },
  ): Promise<void>;
  setEmailPassword(domain: string, username: string, password: string): Promise<void>;
  createForwarder(domain: string, input: { alias: string; destinations: string[] }): Promise<void>;
}

export class MxrouteError extends ServiceError {
  readonly code: string;
  readonly status: number;
  readonly field: string | undefined;

  constructor(status: number, code: string, message: string, field?: string) {
    super(`MXroute ${code} (${status}): ${message}${field ? ` [field: ${field}]` : ""}`);
    this.name = "MxrouteError";
    this.status = status;
    this.code = code;
    this.field = field;
  }
}

export interface MxrouteCredentials {
  server: string;
  username: string;
  apiKey: string;
}

export class HttpMxroute implements Mxroute {
  readonly #creds: MxrouteCredentials;
  readonly #baseUrl: string;
  readonly #fetch: typeof fetch;

  constructor(creds: MxrouteCredentials, opts: { baseUrl?: string; fetch?: typeof fetch } = {}) {
    this.#creds = creds;
    this.#baseUrl = opts.baseUrl ?? "https://api.mxroute.com";
    this.#fetch = opts.fetch ?? fetch;
  }

  async #request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.#fetch(`${this.#baseUrl}${path}`, {
      method,
      headers: {
        "X-Server": this.#creds.server,
        "X-Username": this.#creds.username,
        "X-API-Key": this.#creds.apiKey,
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });

    if (res.status === 204) return undefined as T;

    const text = await res.text();
    let json: { success?: boolean; data?: unknown; error?: { code?: string; message?: string; field?: string } };
    try {
      json = text ? JSON.parse(text) : {};
    } catch {
      throw new MxrouteError(res.status, "BAD_RESPONSE", `not JSON: ${text.slice(0, 200)}`);
    }

    if (!res.ok || json.success === false) {
      const err = json.error ?? {};
      const retry = res.headers.get("Retry-After");
      const message = (err.message ?? res.statusText) + (retry ? ` (retry after ${retry}s)` : "");
      throw new MxrouteError(res.status, err.code ?? "HTTP_ERROR", message, err.field);
    }
    return json.data as T;
  }

  #d(domain: string): string {
    return `/domains/${encodeURIComponent(domain)}`;
  }

  listDomains() {
    return this.#request<string[]>("GET", "/domains");
  }

  getDnsInfo(domain: string) {
    return this.#request<DnsInfo>("GET", `${this.#d(domain)}/dns`);
  }

  getVerificationKey() {
    return this.#request<VerificationKey>("GET", "/verification-key");
  }

  listEmailAccounts(domain: string) {
    return this.#request<EmailAccount[]>("GET", `${this.#d(domain)}/email-accounts`);
  }

  listForwarders(domain: string) {
    return this.#request<Forwarder[]>("GET", `${this.#d(domain)}/forwarders`);
  }

  async addDomain(domain: string) {
    await this.#request("POST", "/domains", { domain });
  }

  async createEmailAccount(
    domain: string,
    input: { username: string; password: string; quota: number; limit: number },
  ) {
    await this.#request("POST", `${this.#d(domain)}/email-accounts`, input);
  }

  async setEmailPassword(domain: string, username: string, password: string) {
    await this.#request("PATCH", `${this.#d(domain)}/email-accounts/${encodeURIComponent(username)}`, {
      password,
    });
  }

  async createForwarder(domain: string, input: { alias: string; destinations: string[] }) {
    await this.#request("POST", `${this.#d(domain)}/forwarders`, input);
  }
}
