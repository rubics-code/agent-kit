import { type CloudflareDns, CloudflareError, type DnsRecord, type NewDnsRecord, type Zone } from "./client.ts";

export type SeedRecord = Omit<DnsRecord, "id" | "ttl"> & { ttl?: number };

// In-memory Cloudflare DNS for tests and evals. It records every write.
export class MemoryCloudflareDns implements CloudflareDns {
  readonly zones = new Map<string, { zone: Zone; records: DnsRecord[] }>();
  readonly writes: { op: string; args: unknown }[] = [];
  #next = 1;

  constructor(seed: Record<string, SeedRecord[]> = {}) {
    for (const [name, records] of Object.entries(seed)) {
      this.zones.set(name, {
        zone: { id: `zone-${name}`, name },
        records: records.map((r) => ({ ttl: 1, ...r, id: `rec-${this.#next++}` })),
      });
    }
  }

  #byId(zoneId: string) {
    const z = [...this.zones.values()].find((v) => v.zone.id === zoneId);
    if (!z) throw new CloudflareError(404, [7003], `zone ${zoneId} not found`);
    return z;
  }

  async findZone(domain: string) {
    const labels = domain.split(".");
    for (let i = 0; i < labels.length - 1; i++) {
      const z = this.zones.get(labels.slice(i).join("."));
      if (z) return z.zone;
    }
    return null;
  }

  async listRecords(zoneId: string) {
    return this.#byId(zoneId).records;
  }

  async createRecord(zoneId: string, record: NewDnsRecord) {
    this.writes.push({ op: "createRecord", args: { zoneId, ...record } });
    const z = this.#byId(zoneId);
    if (
      z.records.some(
        (r) => r.type === record.type && r.name === record.name && r.content === record.content,
      )
    ) {
      throw new CloudflareError(400, [81058], "An identical record already exists.");
    }
    const created: DnsRecord = { ...record, id: `rec-${this.#next++}`, ttl: 1 };
    z.records.push(created);
    return created;
  }
}
