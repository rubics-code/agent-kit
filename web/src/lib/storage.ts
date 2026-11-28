// Keeps the connected agents and the threads in the browser. It never keeps a
// key: the agents hold their own keys.
import type { Thread } from "./thread";

export type Agent = {
  id: string;
  name: string;
  base: string; // the address that the person entered
  endpoint: string; // the A2A JSON-RPC URL
  description?: string;
  a2ui: boolean;
  connectedAt: number;
};

const AGENTS = "console.agents.v1";
const THREADS = "console.threads.v1";
const MAX_THREADS = 200;

function read<T>(store: Storage, key: string, fallback: T): T {
  try {
    const raw = store.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

export function loadAgents(store: Storage = localStorage): Agent[] {
  return read<Agent[]>(store, AGENTS, []);
}

export function saveAgents(agents: Agent[], store: Storage = localStorage): void {
  store.setItem(AGENTS, JSON.stringify(agents));
}

export function upsertAgent(agents: Agent[], a: Agent): Agent[] {
  const i = agents.findIndex((x) => x.id === a.id);
  if (i < 0) return [...agents, a];
  const next = [...agents];
  next[i] = { ...agents[i], ...a, connectedAt: agents[i]!.connectedAt };
  return next;
}

export function loadThreads(store: Storage = localStorage): Thread[] {
  return read<Thread[]>(store, THREADS, []).map((t) => ({ ...t, streaming: undefined }));
}

// saveThreads keeps the newest threads, so the store stays small.
export function saveThreads(threads: Thread[], store: Storage = localStorage): void {
  const keep = [...threads].sort((a, b) => b.updatedAt - a.updatedAt).slice(0, MAX_THREADS);
  store.setItem(THREADS, JSON.stringify(keep.map(({ streaming: _, ...t }) => t)));
}

// agentId gives a stable id for an agent address.
export function agentId(base: string): string {
  const u = new URL(base);
  return (u.host + u.pathname).replace(/\/+$/, "").toLowerCase();
}
