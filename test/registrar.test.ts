import { AIMessage, fakeModel } from "langchain";
import { Command } from "@langchain/langgraph";
import { describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { type ApprovalRequest, HttpCloudflareRegistrar, isApprovalRequest, MemoryCloudflareRegistrar } from "../src/index.ts";

const price = (c: string) => ({ currency: "USD", registration_cost: c, renewal_cost: c });

function offers() {
  return new MemoryCloudflareRegistrar([
    { name: "rubicsdemo.com", registrable: true, tier: "standard", pricing: price("10.44") },
    { name: "rubics.ai", registrable: false, tier: "premium", reason: "domain_premium" },
  ]);
}

function setup(model: ReturnType<typeof fakeModel>, reg = offers()) {
  const { tools, briefing } = assemble({
    services: { registrar: reg },
    secrets: { deliver: async () => {} },
    enable: { has: () => true },
  });
  const ruby = createRuby({ model, tools, briefing });
  return { reg, ruby, config: { configurable: { thread_id: crypto.randomUUID() } } };
}

function approvals(state: { __interrupt__?: { id?: string; value?: unknown }[] }) {
  return (state.__interrupt__ ?? []).filter((i) => isApprovalRequest(i.value)) as { id: string; value: ApprovalRequest }[];
}

const text = (s: { messages: { content: unknown }[] }) => JSON.stringify(s.messages.map((m) => m.content));
const register = (d: string) => ({ name: "cloudflare_register_domain", args: { domain: d }, id: "r1" });

async function approveAndRun(model: ReturnType<typeof fakeModel>, reg = offers()) {
  const { ruby, config } = setup(model, reg);
  const first = await ruby.invoke({ messages: [{ role: "user", content: "buy it" }] }, config);
  const [req] = approvals(first);
  const done = await ruby.invoke(
    new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
    config,
  );
  return { reg, req: req!, done };
}

describe("Cloudflare Registrar tools", () => {
  it("searches with no approval", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "cloudflare_search_domains", args: { query: "rubics" }, id: "s1" }])
      .respond(new AIMessage("found"));
    const { ruby, config } = setup(model);
    const state = await ruby.invoke({ messages: [{ role: "user", content: "find" }] }, config);
    expect(approvals(state)).toHaveLength(0);
    expect(text(state)).toContain("10.44");
  });

  it("stops before a registration and shows the live price", async () => {
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("ok"));
    const { reg, ruby, config } = setup(model);
    const state = await ruby.invoke({ messages: [{ role: "user", content: "buy" }] }, config);
    const [req] = approvals(state);
    expect(req?.value.summary).toContain("10.44 USD now, renewal 10.44 USD");
    expect(reg.writes).toHaveLength(0);
  });

  it("registers once after approval", async () => {
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("done"));
    const { reg, done } = await approveAndRun(model);
    expect(reg.writes).toEqual([{ op: "register", args: { domain: "rubicsdemo.com" } }]);
    expect(text(done)).toContain("Registered rubicsdemo.com");
  });

  it("does not register when the price changes after approval", async () => {
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("ok"));
    const reg = offers();
    const { ruby, config } = setup(model, reg);
    const first = await ruby.invoke({ messages: [{ role: "user", content: "buy" }] }, config);
    const [req] = approvals(first);
    reg.offers.set("rubicsdemo.com", { name: "rubicsdemo.com", registrable: true, tier: "standard", pricing: price("99.00") });
    await ruby.invoke(new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }), config);
    expect(reg.writes).toHaveLength(0);
  });

  it("does not register when Greg rejects", async () => {
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("ok"));
    const { reg, ruby, config } = setup(model);
    const first = await ruby.invoke({ messages: [{ role: "user", content: "buy" }] }, config);
    const [req] = approvals(first);
    await ruby.invoke(new Command({ resume: { [req!.id]: { approved: false, argsHash: req!.value.argsHash, reason: "no" } } }), config);
    expect(reg.writes).toHaveLength(0);
  });

  it("sends premium and dashboard-only domains to the dashboard, with no approval", async () => {
    for (const d of ["rubics.ai", "rubics.sh"]) {
      const model = fakeModel().respondWithTools([register(d)]).respond(new AIMessage("ok"));
      const { reg, ruby, config } = setup(model);
      const state = await ruby.invoke({ messages: [{ role: "user", content: "buy" }] }, config);
      expect(approvals(state)).toHaveLength(0);
      expect(text(state)).toContain("Cloudflare dashboard");
      expect(reg.writes).toHaveLength(0);
    }
  });

  it("polls a pending registration until it finishes", async () => {
    const reg = offers();
    reg.states = [
      { state: "pending", completed: false },
      { state: "in_progress", completed: false },
      { state: "succeeded", completed: true, context: { registration: { expires_at: "2027-11-27" } } },
    ];
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("done"));
    const { done } = await approveAndRun(model, reg);
    expect(text(done)).toContain("expires 2027-11-27");
  });

  it("reports action_required and says not to register again", async () => {
    const reg = offers();
    reg.states = [{ state: "action_required", completed: false, context: { action: "registrant_email_confirmation_pending" } }];
    const model = fakeModel().respondWithTools([register("rubicsdemo.com")]).respond(new AIMessage("ok"));
    const { done } = await approveAndRun(model, reg);
    expect(text(done)).toContain("registrant_email_confirmation_pending");
    expect(text(done)).toContain("Do not register again");
  });
});

describe("HttpCloudflareRegistrar", () => {
  function fake(routes: Record<string, unknown>) {
    const calls: { method: string; url: string; body?: unknown }[] = [];
    const f = (async (url: string, init: RequestInit) => {
      calls.push({ method: init.method!, url, body: init.body ? JSON.parse(String(init.body)) : undefined });
      const key = Object.keys(routes).find((k) => url.includes(k));
      return new Response(JSON.stringify({ success: true, result: key ? routes[key] : null }), { status: key === "/registrations" ? 202 : 200 });
    }) as typeof fetch;
    return { calls, f };
  }

  it("finds the one account and sends a one-year registration with no auto-renew", async () => {
    const { calls, f } = fake({ "/registrations": { state: "pending", completed: false }, "/accounts": [{ id: "acc1" }] });
    const reg = new HttpCloudflareRegistrar("t", { fetch: f, baseUrl: "https://cf" });
    await reg.register("rubicsdemo.com");
    expect(calls.at(-1)).toEqual({
      method: "POST",
      url: "https://cf/accounts/acc1/registrar/registrations",
      body: { domain_name: "rubicsdemo.com", years: 1, auto_renew: false, privacy_mode: "redaction" },
    });
  });

  it("refuses a token that sees more than one account", async () => {
    const { f } = fake({ "/accounts": [{ id: "a" }, { id: "b" }] });
    const reg = new HttpCloudflareRegistrar("t", { fetch: f, baseUrl: "https://cf" });
    await expect(reg.search("x")).rejects.toThrow(/exactly one/);
  });
});
