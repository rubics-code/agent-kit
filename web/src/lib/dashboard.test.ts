import { describe, expect, it } from "vitest";
import { addCard, cardIdFor, fetchCards, kitURL, lastChange, type DashCard } from "./dashboard";
import type { Agent } from "./storage";

const agent: Agent = { id: "a", name: "devagent", base: "http://127.0.0.1:8080", endpoint: "/a2a", a2ui: true, connectedAt: 0 };
const card = (id: string, created: string, updated: string, source: "agent" | "person" = "agent"): DashCard => ({ cardId: id, title: id, a2ui: [], source, createdAt: created, updatedAt: updated, agentId: "a", agentName: "devagent" });

describe("dashboard", () => {
  it("uses a relative path for the page's own agent", () => {
    expect(kitURL(agent, "/kit/dashboard", "http://127.0.0.1:8080")).toBe("/kit/dashboard");
    expect(kitURL(agent, "/kit/dashboard", "http://localhost:5173")).toBe("http://127.0.0.1:8080/kit/dashboard");
  });
  it("tags each card with its agent, and treats 404 as no dashboard", async () => {
    const ok = (async () => new Response(JSON.stringify({ cards: [card("x", "t", "t")] }))) as unknown as typeof fetch;
    expect((await fetchCards(agent, "http://o", ok))[0]).toMatchObject({ cardId: "x", agentName: "devagent" });
    const none = (async () => new Response("", { status: 404 })) as unknown as typeof fetch;
    expect(await fetchCards(agent, "http://o", none)).toEqual([]);
  });
  it("posts a card", async () => {
    let sent: RequestInit | undefined;
    const f = (async (_u: unknown, init?: RequestInit) => ((sent = init), new Response("{}"))) as typeof fetch;
    await addCard(agent, { cardId: "n-1", title: "Notes", a2ui: [] }, "http://o", f);
    expect(sent?.method).toBe("POST");
    expect(JSON.parse(String(sent?.body))).toMatchObject({ cardId: "n-1", title: "Notes" });
  });
  it("says what changed last", () => {
    const a = card("old", "2026-11-28T01:00:00Z", "2026-11-28T01:00:00Z");
    const b = card("new", "2026-11-28T01:00:00Z", "2026-11-28T02:00:00Z");
    expect(lastChange([a, b])?.text).toBe("devagent updated “new”");
    expect(lastChange([card("p", "2026-11-28T03:00:00Z", "2026-11-28T03:00:00Z", "person")])?.text).toBe("a person added “p”");
    expect(lastChange([])).toBeUndefined();
  });
  it("makes a card id from a title", () => {
    expect(cardIdFor("Notes & Lists!", "Call-AB12-cd")).toBe("notes-lists-callab12");
  });
});
