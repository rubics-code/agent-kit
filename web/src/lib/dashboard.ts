// The workspace dashboard: the cards of every connected agent, newest first.
import type { Agent } from "./storage";

export type DashCard = {
  cardId: string;
  title: string;
  threadId?: string;
  a2ui: unknown[];
  source: "agent" | "person";
  createdAt: string;
  updatedAt: string;
  agentId: string;
  agentName: string;
};

export function kitURL(agent: Agent, path: string, pageOrigin: string): string {
  const base = new URL(agent.base);
  return base.origin === pageOrigin ? path : new URL(path, base).toString();
}

export async function fetchCards(agent: Agent, pageOrigin: string, fetchFn: typeof fetch = (...a) => fetch(...a)): Promise<DashCard[]> {
  const res = await fetchFn(kitURL(agent, "/kit/dashboard", pageOrigin));
  if (res.status === 404) return [];
  if (!res.ok) throw new Error(`${agent.name}: the dashboard answered ${res.status}`);
  const body = (await res.json()) as { cards?: Omit<DashCard, "agentId" | "agentName">[] };
  return (body.cards ?? []).map((c) => ({ ...c, agentId: agent.id, agentName: agent.name }));
}

export async function addCard(agent: Agent, card: { cardId: string; title: string; a2ui: unknown[]; threadId?: string }, pageOrigin: string, fetchFn: typeof fetch = (...a) => fetch(...a)): Promise<void> {
  const res = await fetchFn(kitURL(agent, "/kit/dashboard", pageOrigin), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(card),
  });
  if (!res.ok) throw new Error(`the agent did not keep the card: ${(await res.text()).trim()}`);
}

export async function removeCard(agent: Agent, cardId: string, pageOrigin: string, fetchFn: typeof fetch = (...a) => fetch(...a)): Promise<void> {
  const res = await fetchFn(kitURL(agent, `/kit/dashboard/${encodeURIComponent(cardId)}`, pageOrigin), { method: "DELETE" });
  if (!res.ok) throw new Error(`the agent did not remove the card (${res.status})`);
}

export function sortCards(cards: DashCard[]): DashCard[] {
  return [...cards].sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
}

export type Change = { at: number; text: string };

// lastChange says what changed last on the dashboard.
export function lastChange(cards: DashCard[]): Change | undefined {
  const c = sortCards(cards)[0];
  if (!c) return undefined;
  const verb = c.createdAt === c.updatedAt ? "added" : "updated";
  const who = c.source === "person" ? "a person" : c.agentName;
  return { at: Date.parse(c.updatedAt), text: `${who} ${verb} “${c.title}”` };
}

export function cardIdFor(title: string, salt: string): string {
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40) || "card";
  return `${slug}-${salt.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 8)}`;
}
