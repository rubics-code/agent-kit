// Client for the Cloudflare Registrar API, v4. Only the calls the tools need.
import { CloudflareError } from "../cloudflare/client.ts";

export interface DomainPricing {
  currency: string;
  registration_cost: string;
  renewal_cost: string;
}

export interface DomainOffer {
  name: string;
  registrable: boolean;
  tier?: "standard" | "premium";
  pricing?: DomainPricing;
  reason?: string;
}

export type RegistrationState = "pending" | "in_progress" | "action_required" | "blocked" | "succeeded" | "failed";

export interface RegistrationStatus {
  state: RegistrationState;
  completed: boolean;
  context?: { domain_name?: string; action?: string; confirmation_sent_to?: string; registration?: { expires_at?: string } };
  error?: { code?: string; message?: string };
}

export interface CloudflareRegistrar {
  // The wait between status polls. Tests set 0.
  pollDelayMs?: number;
  search(query: string, limit?: number): Promise<DomainOffer[]>;
  // Live registry check. Not cached.
  check(domains: string[]): Promise<DomainOffer[]>;
  // One year, no auto-renew, WHOIS redaction, the account's default contact.
  register(domain: string): Promise<RegistrationStatus>;
  status(domain: string): Promise<RegistrationStatus>;
}

interface Envelope<T> {
  success?: boolean;
  result?: T;
  errors?: { code?: number; message?: string }[];
}

export class HttpCloudflareRegistrar implements CloudflareRegistrar {
  readonly #token: string;
  readonly #baseUrl: string;
  readonly #fetch: typeof fetch;
  #accountId: string | undefined;

  constructor(token: string, opts: { baseUrl?: string; fetch?: typeof fetch; accountId?: string } = {}) {
    this.#token = token;
    this.#baseUrl = opts.baseUrl ?? "https://api.cloudflare.com/client/v4";
    this.#fetch = opts.fetch ?? fetch;
    this.#accountId = opts.accountId;
  }

  async #request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.#fetch(`${this.#baseUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${this.#token}`,
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let json: Envelope<T>;
    try {
      json = text ? JSON.parse(text) : {};
    } catch {
      throw new CloudflareError(res.status, [], `not JSON: ${text.slice(0, 200)}`);
    }
    if (!res.ok || json.success === false) {
      const errors = json.errors ?? [];
      const message = errors.map((e) => e.message).filter(Boolean).join("; ") || res.statusText;
      throw new CloudflareError(res.status, errors.map((e) => e.code).filter((c): c is number => typeof c === "number"), message);
    }
    if (json.result === undefined) throw new CloudflareError(res.status, [], "no result in the response");
    return json.result;
  }

  // The token must see exactly one account, so a purchase cannot go to the wrong one.
  async #account(): Promise<string> {
    if (this.#accountId) return this.#accountId;
    const accounts = await this.#request<{ id: string }[]>("GET", "/accounts");
    if (accounts.length !== 1) {
      throw new CloudflareError(400, [], `the token sees ${accounts.length} accounts; it must see exactly one`);
    }
    this.#accountId = accounts[0]!.id;
    return this.#accountId;
  }

  async search(query: string, limit = 20) {
    const a = await this.#account();
    const q = new URLSearchParams({ q: query, limit: String(limit) });
    return (await this.#request<{ domains: DomainOffer[] }>("GET", `/accounts/${a}/registrar/domain-search?${q}`)).domains;
  }

  async check(domains: string[]) {
    const a = await this.#account();
    return (await this.#request<{ domains: DomainOffer[] }>("POST", `/accounts/${a}/registrar/domain-check`, { domains }))
      .domains;
  }

  async register(domain: string) {
    const a = await this.#account();
    return this.#request<RegistrationStatus>("POST", `/accounts/${a}/registrar/registrations`, {
      domain_name: domain,
      years: 1,
      auto_renew: false,
      privacy_mode: "redaction",
    });
  }

  async status(domain: string) {
    const a = await this.#account();
    return this.#request<RegistrationStatus>(
      "GET",
      `/accounts/${a}/registrar/registrations/${encodeURIComponent(domain)}/registration-status`,
    );
  }
}
