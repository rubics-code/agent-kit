import { Command } from "@langchain/langgraph";
import type { AgentGraph } from "./agent.ts";
import { type ApprovalDecision, type ApprovalRequest, isApprovalRequest } from "../approval.ts";

export type Step =
  | { kind: "reply"; text: string }
  | { kind: "approvals"; items: (ApprovalRequest & { id: string })[] };

const config = (thread: string) => ({ configurable: { thread_id: thread }, recursionLimit: 60 });

// One graph run: it ends with a reply, or stops at the approvals it needs.
async function run(agent: AgentGraph, input: Parameters<AgentGraph["invoke"]>[0], thread: string): Promise<Step> {
  const state = await agent.invoke(input, config(thread));
  const pending = (state.__interrupt__ ?? []) as { id: string; value: unknown }[];
  if (pending.length > 0) {
    return {
      kind: "approvals",
      items: pending.map((p) => {
        if (!isApprovalRequest(p.value)) throw new Error(`unknown interrupt: ${JSON.stringify(p.value)}`);
        return { ...p.value, id: p.id };
      }),
    };
  }
  const last = state.messages.at(-1);
  const text = typeof last?.content === "string" ? last.content : JSON.stringify(last?.content);
  return { kind: "reply", text };
}

export const say = (agent: AgentGraph, thread: string, text: string) =>
  run(agent, { messages: [{ role: "user", content: text }] }, thread);

export const resume = (agent: AgentGraph, thread: string, decisions: Record<string, ApprovalDecision>) =>
  run(agent, new Command({ resume: decisions }), thread);
