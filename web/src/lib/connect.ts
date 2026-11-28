import { A2AClient, endpointFor, fetchCard, newId, rendersA2UI, userMessage, type AgentCard, type StreamEvent } from "./a2a";
import { catalogIds } from "./catalogs";
import { agentId, type Agent } from "./storage";

export const TEST_PROMPT = "Say hello in one line.";

// normalizeAddress accepts "host:port" or a full URL, and gives the origin.
export function normalizeAddress(input: string): string {
  const raw = input.trim();
  if (!raw) throw new Error("Enter the agent's address.");
  const url = new URL(/^https?:\/\//i.test(raw) ? raw : `http://${raw}`);
  if (url.username || url.password) throw new Error("Put no name or password in the address. The agent holds its own keys.");
  return url.origin;
}

export function toAgent(card: AgentCard, base: string, pageOrigin: string, now: number): Agent {
  return {
    id: agentId(base),
    name: card.name,
    base,
    endpoint: endpointFor(card, base, pageOrigin),
    description: card.description,
    a2ui: rendersA2UI(card),
    connectedAt: now,
  };
}

// replyText finds the agent's text in a SendMessage result.
export function replyText(ev: StreamEvent): string {
  if ("error" in ev) throw new Error(ev.error);
  if ("message" in ev) return ev.message.parts.map((p) => p.text ?? "").join("").trim();
  if ("task" in ev) {
    const parts = [...(ev.task.artifacts ?? []).flatMap((a) => a.parts), ...(ev.task.status.message?.parts ?? [])];
    return parts.map((p) => p.text ?? "").join("").trim();
  }
  return "";
}

export async function check(base: string, fetchFn?: typeof fetch): Promise<AgentCard> {
  return fetchCard(base, fetchFn);
}

export async function testCall(agent: Agent, fetchFn?: typeof fetch): Promise<{ ms: number; text: string }> {
  const t0 = performance.now();
  const ev = await new A2AClient(agent.endpoint, fetchFn).send(userMessage(`connect-test-${newId()}`, [{ text: TEST_PROMPT }], agent.a2ui ? catalogIds : []));
  const text = replyText(ev);
  if (!text) throw new Error("The agent answered with no text.");
  return { ms: performance.now() - t0, text };
}
