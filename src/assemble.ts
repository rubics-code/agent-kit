// The composition root of an agent: its group list becomes services, tools,
// statuses and the briefing. An agent gives data; it does not wire groups.
import type { StructuredTool } from "@langchain/core/tools";
import { assembleBriefing } from "./briefing.ts";
import { type ConnectContext, type GroupStatus, type SharedDeps, type ToolGroupDef, mountedTools, reviewGroup } from "./group.ts";
import { infisicalGroup } from "./groups/infisical/tools.ts";
import type { AgentPolicy } from "./policy.ts";
import type { SecretStore } from "./secret-store.ts";
import type { SecretSink } from "./secrets.ts";

// Any group. The service type is erased here; each def checks its own.
export type AnyGroup = ToolGroupDef<unknown>;

// The service clients, by group id.
export type Services = Partial<Record<string, unknown>>;

// Which groups are on. `has` answers for a group's env key, `vaultHosts` are
// the hosts of the Agent Vault session, and `hosts` overrides a group's host
// by group id (the Infisical deployment's host is the agent's to name).
export interface Enable {
  has(key: string): boolean;
  vaultHosts?: Set<string> | undefined;
  hosts?: Record<string, string | undefined>;
}

// Makes each group's service from the session's keys.
export function connectServices(groups: readonly AnyGroup[], ctx: ConnectContext): Services {
  return Object.fromEntries(groups.map((def) => [def.id, def.connect(ctx)]));
}

export function assembleAgent(opts: {
  groups: readonly AnyGroup[];
  system: string;
  policy: AgentPolicy;
  services: Services;
  secrets: SecretSink;
  enable: Enable;
}): { tools: StructuredTool[]; briefing: string; statuses: GroupStatus[] } {
  const { groups, policy, services } = opts;
  const statuses = groups.map((def) =>
    reviewGroup(
      def,
      { has: opts.enable.has, hasStore: !!services[infisicalGroup.id], vaultHosts: opts.enable.vaultHosts },
      { host: opts.enable.hosts?.[def.id] },
    ),
  );
  // Mount a service only when its group is on. In Agent Vault mode every key
  // is a placeholder, so a service can exist for a host the session lacks.
  const on = (id: string) => statuses.some((s) => s.id === id && s.enabled);
  const store = on(infisicalGroup.id) ? (services[infisicalGroup.id] as SecretStore | undefined) : undefined;
  const shared: SharedDeps = { secrets: opts.secrets, store, policy };
  const tools = groups.flatMap((def) => mountedTools(def, on(def.id) ? services[def.id] : undefined, shared));
  const briefing = assembleBriefing({ system: opts.system, defs: groups, statuses, policy });
  return { tools, briefing, statuses };
}
