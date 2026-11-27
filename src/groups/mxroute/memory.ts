import {
  type DnsInfo,
  type EmailAccount,
  type Forwarder,
  type Mxroute,
  MxrouteError,
  type VerificationKey,
} from "./client.ts";

// In-memory MXroute for tests and evals. It records every write.
export class MemoryMxroute implements Mxroute {
  readonly domains = new Map<string, { accounts: EmailAccount[]; forwarders: Forwarder[] }>();
  readonly writes: { op: string; args: unknown }[] = [];
  readonly passwords = new Map<string, string>();

  constructor(seed: Record<string, { accounts?: string[]; forwarders?: Record<string, string[]> }> = {}) {
    for (const [domain, s] of Object.entries(seed)) {
      this.domains.set(domain, {
        accounts: (s.accounts ?? []).map((u) => account(domain, u, 1024, 9600)),
        forwarders: Object.entries(s.forwarders ?? {}).map(([alias, destinations]) => ({
          alias,
          email: `${alias}@${domain}`,
          destinations,
        })),
      });
    }
  }

  #get(domain: string) {
    const d = this.domains.get(domain);
    if (!d) throw new MxrouteError(404, "NOT_FOUND", `domain ${domain} not found`);
    return d;
  }

  async listDomains() {
    return [...this.domains.keys()];
  }

  async getDnsInfo(domain: string): Promise<DnsInfo> {
    this.#get(domain);
    return {
      mx_records: [
        { priority: 10, hostname: "sunfire.mxrouting.net" },
        { priority: 20, hostname: "sunfire-relay.mxrouting.net" },
      ],
      spf: { type: "TXT", name: "@", value: "v=spf1 include:mxroute.com -all" },
      dkim: { type: "TXT", name: "x._domainkey", value: "v=DKIM1; k=rsa; p=MIIBFAKEKEY" },
      verification: null,
    };
  }

  async getVerificationKey(): Promise<VerificationKey> {
    const key = "_da-verify-00000000000000000000000000000000";
    return { key, record: { type: "TXT", name: key, value: "domain-verified" } };
  }

  async listEmailAccounts(domain: string) {
    return this.#get(domain).accounts;
  }

  async listForwarders(domain: string) {
    return this.#get(domain).forwarders;
  }

  async addDomain(domain: string) {
    this.writes.push({ op: "addDomain", args: { domain } });
    if (this.domains.has(domain)) throw new MxrouteError(409, "CONFLICT", `${domain} already exists`);
    this.domains.set(domain, { accounts: [], forwarders: [] });
  }

  async createEmailAccount(
    domain: string,
    input: { username: string; password: string; quota: number; limit: number },
  ) {
    this.writes.push({
      op: "createEmailAccount",
      args: { domain, username: input.username, quota: input.quota, limit: input.limit },
    });
    const d = this.#get(domain);
    if (d.accounts.some((a) => a.username === input.username)) {
      throw new MxrouteError(409, "CONFLICT", `${input.username}@${domain} already exists`);
    }
    d.accounts.push(account(domain, input.username, input.quota, input.limit));
    this.passwords.set(`${input.username}@${domain}`, input.password);
  }

  async setEmailPassword(domain: string, username: string, password: string) {
    this.writes.push({ op: "setEmailPassword", args: { domain, username } });
    if (!this.#get(domain).accounts.some((a) => a.username === username)) {
      throw new MxrouteError(404, "NOT_FOUND", `${username}@${domain} not found`);
    }
    this.passwords.set(`${username}@${domain}`, password);
  }

  async createForwarder(domain: string, input: { alias: string; destinations: string[] }) {
    this.writes.push({ op: "createForwarder", args: { domain, ...input } });
    const d = this.#get(domain);
    d.forwarders.push({ alias: input.alias, email: `${input.alias}@${domain}`, destinations: input.destinations });
  }
}

function account(domain: string, username: string, quota: number, limit: number): EmailAccount {
  return { username, email: `${username}@${domain}`, quota, usage: 0, limit, sent: 0, suspended: false };
}
