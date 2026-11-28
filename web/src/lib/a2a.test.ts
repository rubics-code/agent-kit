import { describe, expect, it } from "vitest";
import { endpointFor, fetchCard, rendersA2UI, userMessage, A2UI_EXTENSION, BASIC_CATALOG } from "./a2a";

const card = { name: "dev", supportedInterfaces: [{ url: "http://127.0.0.1:8080/a2a", protocolBinding: "JSONRPC" }], capabilities: { extensions: [{ uri: A2UI_EXTENSION }] } };

describe("a2a helpers", () => {
  it("uses a relative endpoint for the page's own agent", () => {
    expect(endpointFor(card, "http://127.0.0.1:8080", "http://127.0.0.1:8080")).toBe("/a2a");
    expect(endpointFor(card, "http://127.0.0.1:8080", "http://localhost:5173")).toBe("http://127.0.0.1:8080/a2a");
  });
  it("puts the renderer capabilities on each message", () => {
    const m = userMessage("c1", [{ text: "hi" }], [BASIC_CATALOG]);
    expect(m.metadata).toEqual({ a2uiRendererCapabilities: { "1.0": { supportedCatalogIds: [BASIC_CATALOG] } } });
    expect(m.taskId).toBeUndefined();
    expect(userMessage("c1", [], [], "t1").taskId).toBe("t1");
  });
  it("reads a card, and refuses a card with no name", async () => {
    const ok = (body: unknown) => (async () => new Response(JSON.stringify(body))) as unknown as typeof fetch;
    await expect(fetchCard("http://x.test", ok(card))).resolves.toMatchObject({ name: "dev" });
    await expect(fetchCard("http://x.test", ok({}))).rejects.toThrow(/no name/);
    expect(rendersA2UI(card)).toBe(true);
  });
});
