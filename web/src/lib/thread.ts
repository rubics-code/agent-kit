// The thread model, and how A2A stream events change it. Pure functions, so
// that the logic is easy to test.
import { A2UI_MIME, type Artifact, type Part, type StreamEvent } from "./a2a";

export type Approval = { tool: string; summary: string; argsHash: string; args: Record<string, unknown> };

export type Item =
  | { kind: "user"; id: string; text: string; at: number; voice?: boolean }
  | { kind: "agent"; id: string; text: string; at: number }
  | { kind: "tool"; id: string; name: string; state: "running" | "done" | "waiting" | "failed" | "rejected"; at: number; ms?: number }
  | {
      kind: "approval";
      id: string;
      confirmationId: string;
      surfaceId?: string;
      messages: unknown[];
      approval?: Approval;
      decision?: "approved" | "rejected";
      decidedAt?: number;
      at: number;
    }
  | { kind: "surface"; id: string; surfaceId: string; messages: unknown[]; at: number; title?: string; onDashboard?: boolean }
  | { kind: "error"; id: string; text: string; at: number };

export type Thread = {
  id: string; // the A2A contextId
  agentId: string;
  title: string;
  items: Item[];
  updatedAt: number;
  pendingTaskId?: string;
  state?: string;
  streaming?: string; // the reply text while it streams
  dashboardAt?: number; // when this thread last changed the dashboard
};

export function newThread(id: string, agentId: string, now: number): Thread {
  return { id, agentId, title: "New thread", items: [], updatedAt: now };
}

let seq = 0;
const itemId = () => `i${Date.now().toString(36)}${(seq++).toString(36)}`;

export function addUser(t: Thread, text: string, now: number, voice = false): Thread {
  const title = t.items.length === 0 ? titleFrom(text) : t.title;
  return { ...t, title, updatedAt: now, items: [...t.items, { kind: "user", id: itemId(), text, at: now, voice }] };
}

export function titleFrom(text: string): string {
  const one = text.replace(/\s+/g, " ").trim();
  return one.length > 48 ? one.slice(0, 47) + "…" : one || "New thread";
}

export function addError(t: Thread, text: string, now: number): Thread {
  return { ...t, streaming: undefined, updatedAt: now, items: [...t.items, { kind: "error", id: itemId(), text, at: now }] };
}

const isPartial = (m?: Record<string, unknown>) => m?.adk_partial === true;
const adkType = (p: Part) => p.metadata?.adk_type as string | undefined;
type Call = { id?: string; name?: string; args?: Record<string, unknown>; response?: Record<string, unknown> };

// applyEvent folds one stream event into the thread.
export function applyEvent(t: Thread, ev: StreamEvent, now: number): Thread {
  if ("error" in ev) return addError(t, ev.error, now);
  if ("task" in ev) return { ...t, state: ev.task.status.state, updatedAt: now };
  if ("message" in ev) return applyParts(t, ev.message.parts, now);
  if ("artifactUpdate" in ev) return applyArtifact(t, ev.artifactUpdate.artifact, !!ev.artifactUpdate.lastChunk, now);
  const { status, taskId } = ev.statusUpdate;
  let next: Thread = { ...t, state: status.state, updatedAt: now };
  if (status.state === "TASK_STATE_INPUT_REQUIRED") {
    next = { ...next, pendingTaskId: taskId };
    next = applyInputRequired(next, status.message?.parts ?? [], now);
  } else if (isFinal(status.state)) {
    next = { ...next, pendingTaskId: undefined, streaming: undefined };
    next = { ...next, items: next.items.map((i) => (i.kind === "tool" && i.state === "running" ? { ...i, state: "done" } : i)) };
    if (status.state === "TASK_STATE_FAILED") {
      const why = status.message?.parts.map((p) => p.text).filter(Boolean).join(" ");
      next = addError(next, why || "The agent failed.", now);
    }
  }
  return next;
}

export function isFinal(state: string): boolean {
  return ["TASK_STATE_COMPLETED", "TASK_STATE_FAILED", "TASK_STATE_CANCELED", "TASK_STATE_REJECTED"].includes(state);
}

function applyArtifact(t: Thread, a: Artifact, last: boolean, now: number): Thread {
  if (isPartial(a.metadata)) {
    if (last) return { ...t, streaming: undefined };
    const delta = a.parts.map((p) => p.text ?? "").join("");
    return { ...t, streaming: (t.streaming ?? "") + delta, updatedAt: now };
  }
  return applyParts({ ...t, streaming: undefined }, a.parts, now);
}

function applyParts(t: Thread, parts: Part[], now: number): Thread {
  const items = [...t.items];
  let dashboardChanged = false;
  for (const p of parts) {
    const type = adkType(p);
    if (type === "function_call") {
      const c = p.data as Call;
      if (c.name === "adk_request_confirmation") continue;
      items.push({ kind: "tool", id: itemId(), name: c.name ?? "tool", state: "running", at: now });
    } else if (type === "function_response") {
      const c = p.data as Call;
      const i = findLastIndex(items, (x) => x.kind === "tool" && x.name === c.name && (x.state === "running" || x.state === "waiting"));
      if (i >= 0) {
        const tool = items[i] as Extract<Item, { kind: "tool" }>;
        const st = String(c.response?.status ?? "");
        const err = c.response?.error ? String(c.response.error) : "";
        const state = err.includes("rejected") ? "rejected" : err ? "failed" : st.startsWith("waiting") ? "waiting" : "done";
        items[i] = { ...tool, state, ms: now - tool.at };
      }
      const card = c.response?.card as { title?: string; a2ui?: unknown[] } | undefined;
      if (card?.a2ui?.length) {
        items.push({ kind: "surface", id: itemId(), surfaceId: surfaceOf(card.a2ui) ?? itemId(), messages: card.a2ui, title: card.title, at: now });
      }
      if (c.name === "put_dashboard_card" && !c.response?.error) dashboardChanged = true;
    } else if (p.metadata?.mimeType === A2UI_MIME) {
      const messages = (p.data as unknown[]) ?? [];
      items.push({ kind: "surface", id: itemId(), surfaceId: surfaceOf(messages) ?? itemId(), messages, at: now });
    } else if (p.text) {
      const prev = items.at(-1);
      if (prev?.kind === "agent") items[items.length - 1] = { ...prev, text: prev.text + p.text };
      else items.push({ kind: "agent", id: itemId(), text: p.text, at: now });
    }
  }
  return { ...t, items, updatedAt: now, ...(dashboardChanged ? { dashboardAt: now } : {}) };
}

export function markOnDashboard(t: Thread, itemId: string): Thread {
  return { ...t, items: t.items.map((i) => (i.id === itemId && i.kind === "surface" ? { ...i, onDashboard: true } : i)) };
}

function applyInputRequired(t: Thread, parts: Part[], now: number): Thread {
  const items = [...t.items];
  const cards = parts.filter((p) => p.metadata?.mimeType === A2UI_MIME).map((p) => (p.data as unknown[]) ?? []);
  for (const p of parts) {
    if (adkType(p) !== "function_call") continue;
    const c = p.data as Call;
    if (c.name !== "adk_request_confirmation" || !c.id) continue;
    if (items.some((i) => i.kind === "approval" && i.confirmationId === c.id)) continue;
    const tc = c.args?.toolConfirmation as { payload?: Approval } | undefined;
    const messages = cards.find((m) => surfaceOf(m)?.endsWith(c.id!)) ?? [];
    items.push({ kind: "approval", id: itemId(), confirmationId: c.id, surfaceId: surfaceOf(messages), messages, approval: tc?.payload, at: now });
  }
  return { ...t, items };
}

export function surfaceOf(messages: unknown[]): string | undefined {
  for (const m of messages as { createSurface?: { surfaceId?: string } }[]) {
    if (m?.createSurface?.surfaceId) return m.createSurface.surfaceId;
  }
  return undefined;
}

export function decide(t: Thread, confirmationId: string, decision: "approved" | "rejected", now: number): Thread {
  return {
    ...t,
    updatedAt: now,
    items: t.items.map((i) => (i.kind === "approval" && i.confirmationId === confirmationId ? { ...i, decision, decidedAt: now } : i)),
  };
}

function findLastIndex<T>(a: T[], f: (x: T) => boolean): number {
  for (let i = a.length - 1; i >= 0; i--) if (f(a[i]!)) return i;
  return -1;
}
