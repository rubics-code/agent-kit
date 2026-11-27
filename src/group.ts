// One self-describing tool group: everything the rest of the system needs to
// know about a group lives in its definition. The capabilities list, the Agent
// Vault host check, the tools panel and the briefing are derived from these
// definitions, so an id, a key or an approval flag is written once, here.
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { requireApproval } from "./approval.ts";
import type { AgentPolicy } from "./policy.ts";
import { rejected, result } from "./result.ts";
import type { SecretSink } from "./secrets.ts";
import type { SecretStore } from "./secret-store.ts";

// What a tool factory gets besides its service client.
export interface SharedDeps {
  secrets: SecretSink;
  store?: SecretStore | undefined;
  policy: AgentPolicy;
}

export interface ToolMeta {
  name: string;
  approval: boolean;
}

// What a group's tools factory receives for its service. The service may be
// any client adapter; the definition never calls it at build time.
export interface ToolGroupDef<S> {
  id: string;
  // Display name, for example MXroute.
  name: string;
  // The service's host, for the Agent Vault session check.
  host: string;
  // Env keys that turn the group on outside Agent Vault mode.
  keys: readonly string[];
  // What to do when the group is off.
  setup: string;
  // The group's briefing text, loaded from guidance.md beside the definition.
  guidance: string;
  // A group may override enablement when its keys are not env keys (the
  // Infisical group turns on with a store, not with a key).
  review?(env: EnableEnv): { enabled: boolean; missing: string[]; setup?: string };
  // Makes the service client from the session's keys, or returns undefined
  // when a key is missing. With it, one entry in a group list adds a group.
  connect(ctx: ConnectContext): S | undefined;
  tools(service: S, shared: SharedDeps): StructuredTool[];
}

// What connect gets. In Agent Vault mode `get` returns a placeholder that the
// proxy replaces, so connect must never read or log the value.
export interface ConnectContext {
  holds(key: string): boolean;
  get(key: string): string;
  store?: SecretStore | undefined;
}

// Whether the session holds each key in the list.
export function holdsAll(ctx: ConnectContext, keys: readonly string[]): boolean {
  return keys.every((k) => ctx.holds(k));
}

// A write tool. The gate is not optional and cannot be skipped: the tool body
// always calls requireApproval before run, with a summary that code composes
// from the arguments. prepare does the reads the summary needs, may normalize
// what the gate hashes (args), may hand data to run, and may return an early
// result instead of gating (nothing to do, or a precondition failed).
const APPROVED = Symbol("kit.approved");

export function approvedTool<S extends z.ZodType, D = undefined>(def: {
  name: string;
  description: string;
  // The policy's operator, for the rejection copy.
  operator: string;
  schema: S;
  prepare: (args: z.infer<S>) => Promise<{
    summary: string;
    // What the gate hashes, when it differs from the raw arguments.
    args?: Record<string, unknown>;
    data?: D;
    early?: string;
  }>;
  run: (args: z.infer<S>, data: D) => Promise<unknown>;
}): StructuredTool {
  const t = tool(
    (args: z.infer<S>) =>
      result(async () => {
        const pre = await def.prepare(args);
        if (pre.early !== undefined) return pre.early;
        const gate = requireApproval(def.name, pre.summary, pre.args ?? (args as Record<string, unknown>));
        if (!gate.approved) return rejected(gate.reason, def.operator);
        return def.run(args, pre.data as D);
      }),
    { name: def.name, description: def.description, schema: def.schema },
  );
  Object.defineProperty(t, APPROVED, { value: true });
  return t;
}

// Dependencies used only to build a definition's tool closures for the
// catalog. A definition must never call them: build time makes closures, it
// does no work.
const definitionDeps: SharedDeps = {
  secrets: { async deliver() {} },
  store: undefined,
  policy: {
    agent: "AGENT",
    operator: "OPERATOR",
    maxKeyLimitUsd: 100,
    infisical: {
      readablePath: z.string(),
      writablePath: z.string(),
      envLabel: () => "ENV",
    },
  },
};

// A group's catalog, derived by building its tools once. approval is
// structural: true exactly when the tool was made with approvedTool, so the
// catalog can never drift from the gate calls in the bodies.
export function toolCatalog(tools: readonly StructuredTool[]): ToolMeta[] {
  return tools.map((t) => ({ name: t.name, approval: APPROVED in t }));
}

export function defCatalog(def: ToolGroupDef<unknown>): ToolMeta[] {
  return toolCatalog(def.tools(undefined as never, definitionDeps));
}

// Mounting: the tools of a group whose service is on, none otherwise.
export function mountedTools<S>(
  def: ToolGroupDef<S>,
  service: S | undefined,
  shared: SharedDeps,
): StructuredTool[] {
  return service ? def.tools(service, shared) : [];
}

// Enablement. Outside Agent Vault mode, a group is on when the session holds
// every key it names. In Agent Vault mode the keys are placeholders, so a
// group is on when the session holds a service for its host. `host` is the
// group's host, as the agent may override it (the Infisical deployment's host
// is the agent's to name).
export interface EnableEnv {
  has(key: string): boolean;
  hasStore: boolean;
  vaultHosts?: Set<string> | undefined;
  host?: string | undefined;
}

export interface GroupStatus {
  id: string;
  name: string;
  enabled: boolean;
  missing: string[];
  setup: string;
  catalog: ToolMeta[];
}

// Takes the unknown-typed definition because it only reads the group's
// description (id, name, keys, host, setup, review) and never its service.
export function reviewGroup(def: ToolGroupDef<unknown>, env: EnableEnv, opts: { host?: string | undefined } = {}): GroupStatus {
  // The agent may override a group's host (the Infisical deployment's host is
  // the agent's to name); otherwise the definition's host stands.
  const fullEnv: EnableEnv = { ...env, host: opts.host ?? def.host };
  let enabled: boolean;
  let missing: string[];
  let setup = def.setup;
  if (def.review) {
    const r = def.review(fullEnv);
    enabled = r.enabled;
    missing = r.missing;
    if (r.setup) setup = r.setup;
  } else if (fullEnv.vaultHosts) {
    const on = !!fullEnv.host && fullEnv.vaultHosts.has(fullEnv.host);
    enabled = on;
    missing = on ? [] : fullEnv.host ? [fullEnv.host] : ["the service"];
    setup = "Add an Agent Vault service for this host to the access bundle, then restart the agent.";
  } else {
    missing = def.keys.filter((k) => !fullEnv.has(k));
    enabled = missing.length === 0;
  }
  return { id: def.id, name: def.name, enabled, missing, setup, catalog: defCatalog(def) };
}
