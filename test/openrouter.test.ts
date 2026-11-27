import { AIMessage, fakeModel } from "langchain";
import { Command } from "@langchain/langgraph";
import { describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { type ApprovalRequest, isApprovalRequest } from "../src/index.ts";
import { HttpOpenRouterKeys, OpenRouterError } from "../src/index.ts";
import { MemoryMxroute } from "../src/index.ts";
import { MemoryOpenRouterKeys } from "../src/index.ts";
import type { SecretSink } from "../src/index.ts";

function setup(model: ReturnType<typeof fakeModel>) {
  const keys = new MemoryOpenRouterKeys([{ name: "Ruby", limit: 20, usageMonthly: 3 }]);
  const delivered: { label: string; value: string }[] = [];
  const secrets: SecretSink = { deliver: async (label, value) => void delivered.push({ label, value }) };
  const { tools, briefing } = assemble({
    services: { mxroute: new MemoryMxroute(), openrouter: keys },
    secrets,
    enable: { has: () => true },
  });
  const ruby = createRuby({ model, tools, briefing });
  const config = { configurable: { thread_id: crypto.randomUUID() } };
  return { keys, delivered, ruby, config };
}

function approvals(state: { __interrupt__?: { id?: string; value?: unknown }[] }) {
  return (state.__interrupt__ ?? []).filter((i) => isApprovalRequest(i.value)) as {
    id: string;
    value: ApprovalRequest;
  }[];
}

const createAcme = {
  name: "openrouter_create_key",
  args: { name: "Acme chatbot", limitUsd: 10, expiresOn: "2027-06-30" },
  id: "c1",
};

describe("OpenRouter key tools", () => {
  it("lists keys with no approval", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "openrouter_list_keys", args: {}, id: "l1" }])
      .respond(new AIMessage("one key"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "keys?" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    const listed = JSON.parse(String(state.messages.find((m) => m.type === "tool")?.content));
    expect(listed).toMatchObject([{ name: "Ruby", limit_usd: 20, limit_remaining_usd: 17 }]);
  });

  it("creates a key only after approval, and the key never enters the conversation", async () => {
    const model = fakeModel().respondWithTools([createAcme]).respond(new AIMessage("done"));
    const { keys, delivered, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "key for acme" }] }, config);
    const [req] = approvals(first);
    expect(req?.value.summary).toBe('Create the OpenRouter API key "Acme chatbot" (limit $10.00 monthly until 2027-06-30)');
    expect(keys.writes).toHaveLength(0);

    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    expect(keys.writes).toEqual([
      {
        op: "createKey",
        args: { name: "Acme chatbot", limit: 10, limit_reset: "monthly", expires_at: "2027-06-30T23:59:59Z" },
      },
    ]);
    expect(delivered).toHaveLength(1);
    expect(delivered[0]!.value).toMatch(/^sk-or-v1-/);
    expect(JSON.stringify(state.messages)).not.toContain(delivered[0]!.value);
  });

  it("does not create a key when Greg rejects", async () => {
    const model = fakeModel().respondWithTools([createAcme]).respond(new AIMessage("ok"));
    const { keys, delivered, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "key for acme" }] }, config);
    const [req] = approvals(first);
    await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: false, argsHash: req!.value.argsHash, reason: "no" } } }),
      config,
    );

    expect(keys.writes).toHaveLength(0);
    expect(delivered).toHaveLength(0);
  });

  it("refuses a key with no limit or a limit above the cap", async () => {
    const model = fakeModel()
      .respondWithTools([
        { name: "openrouter_create_key", args: { name: "big" }, id: "b1" },
        { name: "openrouter_create_key", args: { name: "huge", limitUsd: 5000 }, id: "b2" },
      ])
      .respond(new AIMessage("sorry"));
    const { keys, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(keys.writes).toHaveLength(0);
  });

  it("shows the current and new limit, read from OpenRouter, before an update", async () => {
    const hash = "1".padStart(64, "0");
    const model = fakeModel()
      .respondWithTools([{ name: "openrouter_update_key", args: { hash, limitUsd: 40, disabled: true }, id: "u1" }])
      .respond(new AIMessage("done"));
    const { keys, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "raise it" }] }, config);
    const [req] = approvals(first);
    expect(req?.value.summary).toBe(
      'Change the OpenRouter API key "Ruby" (sk-or-v1-001...0001): limit $20.00 to $40.00, disable it',
    );
    expect(keys.writes).toHaveLength(0);

    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );
    expect(keys.writes).toEqual([{ op: "updateKey", args: { hash, limit: 40, disabled: true } }]);
    const reply = state.messages.find((m) => m.getType() === "tool");
    expect(JSON.parse(String(reply?.content))).toMatchObject({ hash, limit_usd: 40, disabled: true });
  });

  it("refuses an update with no change", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "openrouter_update_key", args: { hash: "1".padStart(64, "0") }, id: "u2" }])
      .respond(new AIMessage("sorry"));
    const { keys, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(keys.writes).toHaveLength(0);
  });
});

describe("HttpOpenRouterKeys", () => {
  function recorder(status: number, body: unknown) {
    const calls: { url: string; init: RequestInit }[] = [];
    const fetch = (async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return new Response(JSON.stringify(body), { status });
    }) as unknown as typeof globalThis.fetch;
    return { calls, fetch };
  }

  it("sends the management key as a Bearer token and returns the one-time key", async () => {
    const r = recorder(201, { data: { hash: "abc", name: "x" }, key: "sk-or-v1-secret" });
    const api = new HttpOpenRouterKeys("mgmt-key", { fetch: r.fetch });

    const out = await api.createKey({ name: "x", limit: 5, limit_reset: "monthly" });

    expect(out.key).toBe("sk-or-v1-secret");
    expect(r.calls[0]!.url).toBe("https://openrouter.ai/api/v1/keys");
    expect(r.calls[0]!.init.method).toBe("POST");
    expect((r.calls[0]!.init.headers as Record<string, string>).Authorization).toBe("Bearer mgmt-key");
    expect(JSON.parse(r.calls[0]!.init.body as string)).toEqual({ name: "x", limit: 5, limit_reset: "monthly" });
  });

  it("turns an error body into an OpenRouterError", async () => {
    const r = recorder(403, { error: { code: 403, message: "Management key does not have permission for this request" } });
    const api = new HttpOpenRouterKeys("mgmt-key", { fetch: r.fetch });

    await expect(api.getCredits()).rejects.toThrow(OpenRouterError);
    await expect(api.getCredits()).rejects.toThrow("(403): Management key does not have permission");
  });
});
