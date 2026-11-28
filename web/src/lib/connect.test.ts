import { describe, expect, it } from "vitest";
import { normalizeAddress, replyText, testCall, toAgent } from "./connect";
import { A2UI_EXTENSION } from "./a2a";

describe("connect", () => {
  it("normalizes an address", () => {
    expect(normalizeAddress(" 127.0.0.1:8097 ")).toBe("http://127.0.0.1:8097");
    expect(normalizeAddress("https://agent.example.test/path")).toBe("https://agent.example.test");
    expect(() => normalizeAddress("")).toThrow(/address/);
    expect(() => normalizeAddress("https://me:secret@agent.example.test")).toThrow(/no name or password/);
  });

  it("makes an agent with no secret in it", () => {
    const a = toAgent({ name: "helper", supportedInterfaces: [{ url: "http://127.0.0.1:8097/a2a" }], capabilities: { extensions: [{ uri: A2UI_EXTENSION }] } }, "http://127.0.0.1:8097", "http://127.0.0.1:5173", 1);
    expect(a).toEqual({ id: "127.0.0.1:8097", name: "helper", base: "http://127.0.0.1:8097", endpoint: "http://127.0.0.1:8097/a2a", description: undefined, a2ui: true, connectedAt: 1 });
  });

  it("reads the reply text of a task", () => {
    expect(replyText({ task: { id: "t", contextId: "c", status: { state: "TASK_STATE_COMPLETED" }, artifacts: [{ artifactId: "a", parts: [{ text: "Hello." }] }] } })).toBe("Hello.");
    expect(() => replyText({ error: "nope" })).toThrow("nope");
  });

  it("runs the test call over SendMessage", async () => {
    let body: { method?: string; params?: { message?: { parts?: { text?: string }[] } } } = {};
    const fetchFn = (async (_u: unknown, init?: RequestInit) => {
      body = JSON.parse(String(init?.body));
      return new Response(JSON.stringify({ result: { message: { messageId: "m", role: "ROLE_AGENT", parts: [{ text: "Hello there." }] } } }));
    }) as typeof fetch;
    const r = await testCall({ id: "x", name: "x", base: "http://x", endpoint: "http://x/a2a", a2ui: false, connectedAt: 0 }, fetchFn);
    expect(r.text).toBe("Hello there.");
    expect(body.method).toBe("SendMessage");
    expect(body.params?.message?.parts?.[0]?.text).toBe("Say hello in one line.");
  });
});
