import { request } from "node:http";
import { AIMessage, fakeModel } from "langchain";
import { afterEach, describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { MemoryMxroute } from "../src/index.ts";
import { startWeb } from "../src/index.ts";

const servers: { close(): void }[] = [];
afterEach(() => servers.splice(0).forEach((s) => s.close()));

async function setup(model: ReturnType<typeof fakeModel>, transcribe?: (wav: Buffer) => Promise<string>) {
  const mx = new MemoryMxroute({ "rubicscode.com": { accounts: ["greg"] } });
  const { tools, briefing } = assemble({
    services: { mxroute: mx },
    secrets: { deliver: async () => {} },
    enable: { has: () => true },
  });
  const ruby = createRuby({ model, tools, briefing });
  const web = await startWeb(ruby, {
    port: 0,
    info: { model: "fake", groups: [], keys: "test" },
    ...(transcribe ? { transcribe } : {}),
  });
  servers.push(web.server);

  function post(path: string, body: unknown, headers: Record<string, string> = {}) {
    return new Promise<{ status: number; json: Record<string, unknown> }>((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port: web.port,
          path,
          method: "POST",
          headers: {
            Host: `localhost:${web.port}`,
            "Content-Type": "application/json",
            ...headers,
          },
        },
        (res) => {
          let data = "";
          res.on("data", (c) => (data += c));
          res.on("end", () => resolve({ status: res.statusCode ?? 0, json: data ? JSON.parse(data) : {} }));
        },
      );
      req.on("error", reject);
      req.end(JSON.stringify(body));
    });
  }
  const thread = async () => (await post("/api/thread", {})).json.thread as string;
  return { mx, web, post, thread };
}

describe("web chat server", () => {
  it("refuses a request from another origin", async () => {
    const { post } = await setup(fakeModel());
    expect((await post("/api/thread", {}, { Origin: "https://evil.example" })).status).toBe(403);
    expect((await post("/api/thread", {}, { Origin: "http://localhost:1" })).status).toBe(403);
  });

  it("accepts the own origin, and a request with no origin", async () => {
    const { post, web } = await setup(fakeModel());
    expect((await post("/api/thread", {}, { Origin: `http://localhost:${web.port}` })).status).toBe(200);
    expect((await post("/api/thread", {}, { Origin: `http://127.0.0.1:${web.port}` })).status).toBe(200);
    expect((await post("/api/thread", {})).status).toBe(200);
  });

  it("refuses a request for another host name", async () => {
    const { post } = await setup(fakeModel());
    expect((await post("/api/thread", {}, { Host: "evil.example:80" })).status).toBe(421);
  });

  it("refuses a form post", async () => {
    const { post } = await setup(fakeModel());
    expect((await post("/api/thread", {}, { "Content-Type": "text/plain" })).status).toBe(415);
  });

  it("gives a new thread with the model and the groups", async () => {
    const { post } = await setup(fakeModel());
    const { json } = await post("/api/thread", {});
    expect(json).toMatchObject({ model: "fake", groups: [], keys: "test" });
    expect(json.thread).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("answers a message", async () => {
    const { post, thread } = await setup(fakeModel().respond(new AIMessage("hello Greg")));
    const { json } = await post("/api/say", { thread: await thread(), text: "hi" });
    expect(json).toEqual({ reply: "hello Greg" });
  });

  it("stops at an approval, and writes only after Greg approves", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "mxroute_create_forwarder", args: { domain: "rubicscode.com", alias: "sales", destinations: ["greg@rubicscode.com"] }, id: "f1" }])
      .respond(new AIMessage("done"));
    const { mx, post, thread } = await setup(model);
    const t = await thread();

    const first = await post("/api/say", { thread: t, text: "forward sales@" });
    const approvals = first.json.approvals as { id: string; summary: string }[];
    expect(approvals).toHaveLength(1);
    expect(approvals[0]!.summary).toBe("Forward sales@rubicscode.com to greg@rubicscode.com");
    expect(mx.writes).toHaveLength(0);

    expect((await post("/api/say", { thread: t, text: "and more" })).status).toBe(409);
    expect((await post("/api/decide", { thread: t, decisions: {} })).status).toBe(400);

    const second = await post("/api/decide", { thread: t, decisions: { [approvals[0]!.id]: { approved: true } } });
    expect(second.json).toEqual({ reply: "done" });
    expect(mx.writes.map((w) => w.op)).toEqual(["createForwarder"]);
  });

  it("does not write when Greg rejects", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "mxroute_add_domain", args: { domain: "example.org" }, id: "a1" }])
      .respond(new AIMessage("ok"));
    const { mx, post, thread } = await setup(model);
    const t = await thread();

    const first = await post("/api/say", { thread: t, text: "add example.org" });
    const [a] = first.json.approvals as { id: string }[];
    await post("/api/decide", { thread: t, decisions: { [a!.id]: { approved: false, reason: "not now" } } });

    expect(mx.writes).toHaveLength(0);
  });

  describe("speech to text", () => {
    const wav = (bytes = 100) => {
      const b = Buffer.alloc(44 + bytes);
      b.write("RIFF", 0, "ascii");
      b.write("WAVE", 8, "ascii");
      return b.toString("base64");
    };

    it("turns a WAV into text", async () => {
      const got: Buffer[] = [];
      const { post } = await setup(fakeModel(), async (w) => (got.push(w), "list my keys"));
      const { status, json } = await post("/api/transcribe", { audio: wav() });
      expect(status).toBe(200);
      expect(json).toEqual({ text: "list my keys" });
      expect(got[0]!.length).toBe(144);
    });

    it("refuses audio that is not a WAV", async () => {
      const { post } = await setup(fakeModel(), async () => "x");
      const { status } = await post("/api/transcribe", { audio: Buffer.from("not audio at all, not a wav file").toString("base64") });
      expect(status).toBe(400);
    });

    it("says so when speech to text is not set up", async () => {
      const { post } = await setup(fakeModel());
      expect((await post("/api/transcribe", { audio: wav() })).status).toBe(501);
    });
  });
});

describe("the page", () => {
  it("shows the agent's name", async () => {
    const { page } = await import("../src/index.ts");
    const html = page("Scout");
    expect(html).toContain("<h1>Scout</h1>");
    expect(html).not.toContain("{{NAME}}");
    expect(html).not.toContain("Ruby");
  });

  it("refuses a name that could break the HTML or the script", async () => {
    const { page } = await import("../src/index.ts");
    expect(() => page('x"</script>')).toThrow("must be 1 to 40");
  });
});
