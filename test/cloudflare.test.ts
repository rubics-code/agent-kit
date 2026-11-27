import { AIMessage, fakeModel } from "langchain";
import { Command } from "@langchain/langgraph";
import { describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { type ApprovalRequest, isApprovalRequest } from "../src/index.ts";
import { CloudflareError, HttpCloudflareDns } from "../src/index.ts";
import { MemoryCloudflareDns } from "../src/index.ts";
import { MemoryMxroute } from "../src/index.ts";
import type { SecretSink } from "../src/index.ts";

const secrets: SecretSink = { deliver: async () => {} };

function setup(model: ReturnType<typeof fakeModel>, cf = seeded()) {
  const { tools, briefing } = assemble({
    services: { mxroute: new MemoryMxroute(), cloudflare: cf },
    secrets,
    enable: { has: () => true },
  });
  const ruby = createRuby({ model, tools, briefing });
  const config = { configurable: { thread_id: crypto.randomUUID() } };
  return { cf, ruby, config };
}

function seeded() {
  return new MemoryCloudflareDns({
    "example.org": [
      { type: "A", name: "example.org", content: "192.0.2.1" },
      { type: "CNAME", name: "www.example.org", content: "example.org" },
    ],
    "rubicscode.com": [{ type: "TXT", name: "rubicscode.com", content: '"v=spf1 include:_spf.google.com -all"' }],
  });
}

function approvals(state: { __interrupt__?: { id?: string; value?: unknown }[] }) {
  return (state.__interrupt__ ?? []).filter((i) => isApprovalRequest(i.value)) as {
    id: string;
    value: ApprovalRequest;
  }[];
}

const call = (args: Record<string, unknown>, id = "c1") => ({ name: "cloudflare_create_dns_record", args, id });
const verification = { domain: "example.org", type: "TXT", name: "_da-verify-abc", content: "domain-verified" };

function lastText(state: { messages: { content: unknown }[] }) {
  return JSON.stringify(state.messages.map((m) => m.content));
}

describe("Cloudflare DNS tools", () => {
  it("lists records with no approval", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "cloudflare_list_dns_records", args: { domain: "example.org", type: "CNAME" }, id: "l1" }])
      .respond(new AIMessage("one CNAME"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "records?" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(lastText(state)).toContain("www.example.org");
    expect(lastText(state)).not.toContain("192.0.2.1");
  });

  it("stops before a write and shows the record and what is at that name now", async () => {
    const model = fakeModel().respondWithTools([call(verification)]).respond(new AIMessage("done"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "add it" }] }, config);
    const [req] = approvals(state);

    expect(req?.value.summary).toBe(
      "Create in Cloudflare (zone example.org): TXT _da-verify-abc.example.org domain-verified. Records at that name now: none",
    );
    expect(cf.writes).toHaveLength(0);
  });

  it("writes once after approval, with Ruby's comment", async () => {
    const mx = { domain: "example.org", type: "MX", name: "@", content: "sunfire.mxrouting.net", priority: 10 };
    const model = fakeModel().respondWithTools([call(mx)]).respond(new AIMessage("done"));
    const { cf, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "add MX" }] }, config);
    const [req] = approvals(first);
    expect(req?.value.summary).toContain("Records at that name now: A example.org 192.0.2.1");
    await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    expect(cf.writes).toEqual([
      {
        op: "createRecord",
        args: {
          zoneId: "zone-example.org",
          type: "MX",
          name: "example.org",
          content: "sunfire.mxrouting.net",
          priority: 10,
          comment: "Created by Ruby, approved by Greg",
        },
      },
    ]);
  });

  it("does not write when Greg rejects", async () => {
    const model = fakeModel().respondWithTools([call(verification)]).respond(new AIMessage("ok"));
    const { cf, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "add it" }] }, config);
    const [req] = approvals(first);
    await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: false, argsHash: req!.value.argsHash, reason: "no" } } }),
      config,
    );

    expect(cf.writes).toHaveLength(0);
  });

  it("asks nothing when the same record is already there", async () => {
    const spf = { domain: "rubicscode.com", type: "TXT", name: "@", content: "v=spf1 include:_spf.google.com -all" };
    const model = fakeModel().respondWithTools([call(spf)]).respond(new AIMessage("ok"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "spf" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
    expect(lastText(state)).toContain("Already in Cloudflare");
  });

  it("refuses a second SPF record at a name", async () => {
    const spf = { domain: "rubicscode.com", type: "TXT", name: "@", content: "v=spf1 include:mxroute.com -all" };
    const model = fakeModel().respondWithTools([call(spf)]).respond(new AIMessage("ok"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "spf" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
    expect(lastText(state)).toContain("already has an SPF record");
  });

  it("refuses a record at a CNAME", async () => {
    const txt = { domain: "example.org", type: "TXT", name: "www", content: "hello" };
    const model = fakeModel().respondWithTools([call(txt)]).respond(new AIMessage("ok"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "txt" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
  });

  it("refuses a record type that Terraform owns, before any approval", async () => {
    const a = { domain: "example.org", type: "A", name: "mail", content: "192.0.2.9" };
    const model = fakeModel().respondWithTools([call(a)]).respond(new AIMessage("sorry"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "a" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
  });

  it("refuses an MX record with no priority, before any approval", async () => {
    const mx = { domain: "example.org", type: "MX", name: "@", content: "sunfire.mxrouting.net" };
    const model = fakeModel().respondWithTools([call(mx)]).respond(new AIMessage("sorry"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "mx" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
  });

  it("says so when no zone holds the domain", async () => {
    const txt = { ...verification, domain: "elsewhere.net" };
    const model = fakeModel().respondWithTools([call(txt)]).respond(new AIMessage("ok"));
    const { cf, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "txt" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(cf.writes).toHaveLength(0);
    expect(lastText(state)).toContain("Cloudflare has no zone for elsewhere.net");
  });
});

describe("HttpCloudflareDns", () => {
  function fakeFetch(handler: (url: string, init: RequestInit) => unknown) {
    const calls: { url: string; init: RequestInit }[] = [];
    const f = (async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      calls.push({ url, init: init ?? {} });
      const body = handler(url, init ?? {});
      const status = (body as { status?: number }).status ?? 200;
      return new Response(JSON.stringify(body), { status });
    }) as typeof fetch;
    return { f, calls };
  }

  it("finds the zone of a subdomain by its parent", async () => {
    const { f, calls } = fakeFetch((url) =>
      url.endsWith("name=example.org")
        ? { success: true, result: [{ id: "z1", name: "example.org" }] }
        : { success: true, result: [] },
    );
    const cf = new HttpCloudflareDns("t", { fetch: f });

    expect(await cf.findZone("mail.example.org")).toEqual({ id: "z1", name: "example.org" });
    expect(calls.map((c) => c.url.split("?")[1])).toEqual(["name=mail.example.org", "name=example.org"]);
    expect((calls[0]!.init.headers as Record<string, string>).Authorization).toBe("Bearer t");
  });

  it("reads every page of records", async () => {
    const { f } = fakeFetch((url) => {
      const page = Number(new URL(url).searchParams.get("page"));
      return {
        success: true,
        result: [{ id: `r${page}`, type: "TXT", name: "a.example.org", content: "x", ttl: 1 }],
        result_info: { page, total_pages: 2 },
      };
    });
    const records = await new HttpCloudflareDns("t", { fetch: f }).listRecords("z1");
    expect(records.map((r) => r.id)).toEqual(["r1", "r2"]);
  });

  it("creates a record with an automatic TTL", async () => {
    const { f, calls } = fakeFetch(() => ({
      success: true,
      result: { id: "r9", type: "TXT", name: "a.example.org", content: "x", ttl: 1 },
    }));
    await new HttpCloudflareDns("t", { fetch: f }).createRecord("z1", {
      type: "TXT",
      name: "a.example.org",
      content: "x",
      comment: "c",
    });
    expect(calls[0]!.init.method).toBe("POST");
    expect(JSON.parse(String(calls[0]!.init.body))).toEqual({
      type: "TXT",
      name: "a.example.org",
      content: "x",
      comment: "c",
      ttl: 1,
    });
  });

  it("turns a Cloudflare error into a CloudflareError", async () => {
    const { f } = fakeFetch(() => ({
      status: 403,
      success: false,
      errors: [{ code: 10000, message: "Authentication error" }],
    }));
    const err = await new HttpCloudflareDns("t", { fetch: f }).listRecords("z1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CloudflareError);
    expect((err as CloudflareError).message).toBe("Cloudflare 403 [10000]: Authentication error");
  });
});
