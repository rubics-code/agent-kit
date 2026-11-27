import type { ApiKey, CreateKeyInput, Credits, OpenRouterKeys, UpdateKeyInput } from "./client.ts";
import { OpenRouterError } from "./client.ts";

// In-memory OpenRouter key API for tests and evals. It records every write.
export class MemoryOpenRouterKeys implements OpenRouterKeys {
  readonly keys = new Map<string, ApiKey>();
  readonly writes: { op: string; args: unknown }[] = [];
  readonly values = new Map<string, string>();
  #next = 1;

  constructor(seed: { name: string; limit?: number | null; usageMonthly?: number; disabled?: boolean }[] = []) {
    for (const s of seed) this.#add(s.name, s.limit ?? null, "monthly", s.usageMonthly ?? 0, s.disabled ?? false);
  }

  #add(name: string, limit: number | null, reset: ApiKey["limit_reset"], usage: number, disabled: boolean, expires?: string) {
    const n = this.#next++;
    const hash = n.toString(16).padStart(64, "0");
    const key: ApiKey = {
      hash,
      name,
      label: `sk-or-v1-${hash.slice(-3)}...${hash.slice(-4)}`,
      disabled,
      limit,
      limit_remaining: limit === null ? null : Math.max(0, limit - usage),
      limit_reset: reset,
      usage,
      usage_daily: 0,
      usage_weekly: 0,
      usage_monthly: usage,
      created_at: "2026-11-01T00:00:00Z",
      last_used_at: null,
      expires_at: expires ?? null,
    };
    this.keys.set(hash, key);
    return key;
  }

  async getCredits(): Promise<Credits> {
    return { total_credits: 100, total_usage: 12.5 };
  }

  async listKeys(opts: { includeDisabled?: boolean } = {}) {
    return [...this.keys.values()].filter((k) => opts.includeDisabled || !k.disabled);
  }

  async getKey(hash: string) {
    const key = this.keys.get(hash);
    if (!key) throw new OpenRouterError(404, "Resource not found");
    return key;
  }

  async createKey(input: CreateKeyInput) {
    this.writes.push({ op: "createKey", args: input });
    const data = this.#add(input.name, input.limit, input.limit_reset, 0, false, input.expires_at);
    const key = `sk-or-v1-${data.hash}`;
    this.values.set(data.hash, key);
    return { data, key };
  }

  async updateKey(hash: string, input: UpdateKeyInput) {
    this.writes.push({ op: "updateKey", args: { hash, ...input } });
    const key = await this.getKey(hash);
    Object.assign(key, input);
    return key;
  }
}
