// Client for the Cloudflare DNS API, v4. Only the calls the tools need.
import { ServiceError } from "../../errors.ts";

export interface Zone {
  id: string;
  name: string;
}

export interface DnsRecord {
  id: string;
  type: string;
  name: string; // fully qualified, no trailing dot
  content: string;
  priority?: number;
  ttl: number; // 1 = automatic
  proxied?: boolean;
  comment?: string | null;
}

export interface NewDnsRecord {
  type: "MX" | "TXT";
  name: string; // fully qualified
  content: string;
  priority?: number;
  comment: string;
}

export interface CloudflareDns {
  // The zone that holds `domain`: the domain itself or a parent of it. Null when the token sees none.
  findZone(domain: string): Promise<Zone | null>;
  listRecords(zoneId: string): Promise<DnsRecord[]>;
  createRecord(zoneId: string, record: NewDnsRecord): Promise<DnsRecord>;
}

export class CloudflareError extends ServiceError {
  readonly status: number;
  readonly codes: number[];

  constructor(status: number, codes: number[], message: string) {
    super(`Cloudflare ${status}${codes.length ? ` [${codes.join(", ")}]` : ""}: ${message}`);
    this.name = "CloudflareError";
    this.status = status;
    this.codes = codes;
  }
}

interface Envelope<T> {
  success?: boolean;
  result?: T;
  errors?: { code?: number; message?: string }[];
  result_info?: { page?: number; total_pages?: number };
}

export class HttpCloudflareDns implements CloudflareDns {
  readonly #token: string;
  readonly #baseUrl: string;
  readonly #fetch: typeof fetch;

  constructor(token: string, opts: { baseUrl?: string; fetch?: typeof fetch } = {}) {
    this.#token = token;
    this.#baseUrl = opts.baseUrl ?? "https://api.cloudflare.com/client/v4";
    this.#fetch = opts.fetch ?? fetch;
  }

  async #request<T>(method: string, path: string, body?: unknown): Promise<Envelope<T>> {
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
      throw new CloudflareError(
        res.status,
        errors.map((e) => e.code).filter((c): c is number => typeof c === "number"),
        message,
      );
    }
    return json;
  }

  async findZone(domain: string): Promise<Zone | null> {
    const labels = domain.toLowerCase().split(".");
    for (let i = 0; i < labels.length - 1; i++) {
      const name = labels.slice(i).join(".");
      const { result } = await this.#request<Zone[]>("GET", `/zones?name=${encodeURIComponent(name)}`);
      const zone = result?.find((z) => z.name === name);
      if (zone) return { id: zone.id, name: zone.name };
    }
    return null;
  }

  async listRecords(zoneId: string): Promise<DnsRecord[]> {
    const all: DnsRecord[] = [];
    for (let page = 1; ; page++) {
      const env = await this.#request<DnsRecord[]>(
        "GET",
        `/zones/${encodeURIComponent(zoneId)}/dns_records?per_page=100&page=${page}`,
      );
      all.push(...(env.result ?? []));
      if (page >= (env.result_info?.total_pages ?? 1)) return all;
    }
  }

  async createRecord(zoneId: string, record: NewDnsRecord): Promise<DnsRecord> {
    const { result } = await this.#request<DnsRecord>("POST", `/zones/${encodeURIComponent(zoneId)}/dns_records`, {
      ...record,
      ttl: 1,
    });
    if (!result) throw new CloudflareError(200, [], "no record in the response");
    return result;
  }
}
