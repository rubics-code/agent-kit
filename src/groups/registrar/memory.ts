import type { CloudflareRegistrar, DomainOffer, RegistrationStatus } from "./client.ts";

// In-memory Cloudflare Registrar for tests and evals. It records every write.
export class MemoryCloudflareRegistrar implements CloudflareRegistrar {
  readonly offers: Map<string, DomainOffer>;
  pollDelayMs = 0;
  readonly writes: { op: string; args: unknown }[] = [];
  // The states that register() and then each status() call return, in order.
  states: RegistrationStatus[] = [{ state: "succeeded", completed: true }];

  constructor(offers: DomainOffer[] = []) {
    this.offers = new Map(offers.map((o) => [o.name, o]));
  }

  async search(query: string, limit = 20) {
    return [...this.offers.values()].filter((o) => o.name.includes(query.toLowerCase())).slice(0, limit);
  }

  async check(domains: string[]) {
    return domains.map(
      (name) => this.offers.get(name) ?? { name, registrable: false, reason: "domain_unavailable" },
    );
  }

  async register(domain: string) {
    this.writes.push({ op: "register", args: { domain } });
    return this.#next();
  }

  async status(_domain: string) {
    return this.#next();
  }

  #next(): RegistrationStatus {
    return this.states.length > 1 ? this.states.shift()! : this.states[0]!;
  }
}
