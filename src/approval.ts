import { createHash } from "node:crypto";
import { interrupt } from "@langchain/langgraph";

export interface ApprovalRequest {
  kind: "approval";
  tool: string;
  // Written by code from the arguments, never by the model.
  summary: string;
  args: Record<string, unknown>;
  argsHash: string;
}

export interface ApprovalDecision {
  approved: boolean;
  argsHash: string;
  reason?: string;
}

export function isApprovalRequest(value: unknown): value is ApprovalRequest {
  return typeof value === "object" && value !== null && (value as { kind?: unknown }).kind === "approval";
}

export function hashArgs(args: Record<string, unknown>): string {
  return createHash("sha256").update(canonical(args)).digest("hex");
}

function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, v]) => v !== undefined)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
    return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

// Pauses the graph until the operator decides. The tool body re-runs from the
// start on resume, so nothing with a side effect may come before this call.
export function requireApproval(
  tool: string,
  summary: string,
  args: Record<string, unknown>,
): { approved: true } | { approved: false; reason: string } {
  const request: ApprovalRequest = { kind: "approval", tool, summary, args, argsHash: hashArgs(args) };
  const decision = interrupt<ApprovalRequest, ApprovalDecision | undefined>(request);

  if (!decision || decision.approved !== true) {
    return { approved: false, reason: decision?.reason ?? "rejected" };
  }
  if (decision.argsHash !== request.argsHash) {
    return { approved: false, reason: "the approval does not match these arguments" };
  }
  return { approved: true };
}
