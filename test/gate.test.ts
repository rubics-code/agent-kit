import { AIMessage, fakeModel } from "langchain";
import { Command } from "@langchain/langgraph";
import { describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { type ApprovalRequest, isApprovalRequest } from "../src/index.ts";
import { MemoryMxroute } from "../src/index.ts";
import type { SecretSink } from "../src/index.ts";

function setup(model: ReturnType<typeof fakeModel>) {
  const mx = new MemoryMxroute({ "rubicscode.com": { accounts: ["greg", "tally"] } });
  const delivered: { label: string; value: string }[] = [];
  const secrets: SecretSink = { deliver: async (label, value) => void delivered.push({ label, value }) };
  const { tools, briefing } = assemble({ services: { mxroute: mx }, secrets, enable: { has: () => true } });
  const ruby = createRuby({ model, tools, briefing });
  const config = { configurable: { thread_id: crypto.randomUUID() } };
  return { mx, delivered, ruby, config };
}

function approvals(state: { __interrupt__?: { id?: string; value?: unknown }[] }) {
  return (state.__interrupt__ ?? []).filter((i) => isApprovalRequest(i.value)) as {
    id: string;
    value: ApprovalRequest;
  }[];
}

const createHello = {
  name: "mxroute_create_mailbox",
  args: { domain: "rubicscode.com", username: "hello" },
  id: "call_1",
};

describe("approval gate", () => {
  it("runs a read tool with no approval", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "mxroute_list_mailboxes", args: { domain: "rubicscode.com" }, id: "r1" }])
      .respond(new AIMessage("greg and tally"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "who has a mailbox?" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(state.messages.at(-1)?.content).toBe("greg and tally");
  });

  it("stops before a write and shows a summary made by code", async () => {
    const model = fakeModel().respondWithTools([createHello]).respond(new AIMessage("done"));
    const { mx, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "make hello@" }] }, config);
    const [req] = approvals(state);

    expect(req?.value.tool).toBe("mxroute_create_mailbox");
    expect(req?.value.summary).toBe(
      "Create the mailbox hello@rubicscode.com (quota 1024 MB, send limit 9600 per day)",
    );
    expect(mx.writes).toHaveLength(0);
  });

  it("writes once after approval, and the password never enters the conversation", async () => {
    const model = fakeModel().respondWithTools([createHello]).respond(new AIMessage("done"));
    const { mx, delivered, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "make hello@" }] }, config);
    const [req] = approvals(first);
    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    expect(mx.writes).toEqual([
      { op: "createEmailAccount", args: { domain: "rubicscode.com", username: "hello", quota: 1024, limit: 9600 } },
    ]);
    expect(delivered).toHaveLength(1);
    const password = delivered[0]!.value;
    expect(password).toBe(mx.passwords.get("hello@rubicscode.com"));
    expect(JSON.stringify(state.messages)).not.toContain(password);
  });

  it("does not write when Greg rejects", async () => {
    const model = fakeModel().respondWithTools([createHello]).respond(new AIMessage("ok"));
    const { mx, delivered, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "make hello@" }] }, config);
    const [req] = approvals(first);
    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: false, argsHash: req!.value.argsHash, reason: "not now" } } }),
      config,
    );

    expect(mx.writes).toHaveLength(0);
    expect(delivered).toHaveLength(0);
    expect(JSON.stringify(state.messages)).toContain(
      "Not done: Greg did not approve (not now). Do not try again unless Greg asks.",
    );
  });

  it("lists the domains with no arguments", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "mxroute_list_domains", args: {}, id: "l1" }])
      .respond(new AIMessage("ok"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "domains?" }] }, config);

    expect(JSON.stringify(state.messages)).toContain("rubicscode.com");
    expect(JSON.stringify(state.messages)).not.toContain("Error");
  });

  it("does not write when the approval is for other arguments", async () => {
    const model = fakeModel().respondWithTools([createHello]).respond(new AIMessage("ok"));
    const { mx, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "make hello@" }] }, config);
    const [req] = approvals(first);
    await ruby.invoke(new Command({ resume: { [req!.id]: { approved: true, argsHash: "0".repeat(64) } } }), config);

    expect(mx.writes).toHaveLength(0);
  });

  it("asks for each write when the model calls two at once", async () => {
    const model = fakeModel()
      .respondWithTools([
        createHello,
        { name: "mxroute_create_forwarder", args: { domain: "rubicscode.com", alias: "sales", destinations: ["greg@rubicscode.com"] }, id: "call_2" },
      ])
      .respond(new AIMessage("done"));
    const { mx, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "both" }] }, config);
    const reqs = approvals(first);
    expect(reqs.map((r) => r.value.tool).sort()).toEqual(["mxroute_create_forwarder", "mxroute_create_mailbox"]);

    const resume = Object.fromEntries(
      reqs.map((r) => [r.id, { approved: r.value.tool === "mxroute_create_forwarder", argsHash: r.value.argsHash }]),
    );
    await ruby.invoke(new Command({ resume }), config);

    expect(mx.writes.map((w) => w.op)).toEqual(["createForwarder"]);
  });

  it("refuses arguments outside the schema before any approval", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "mxroute_create_mailbox", args: { domain: "Not A Domain", username: "x" }, id: "bad" }])
      .respond(new AIMessage("sorry"));
    const { mx, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(mx.writes).toHaveLength(0);
  });
});
