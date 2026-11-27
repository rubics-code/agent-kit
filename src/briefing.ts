// The briefing: the agent's system prompt, the guidance of every included
// group, and the capability note. Placeholders in the prose — {{operator}},
// {{agent}}, {{maxKeyLimitUsd}} — are substituted from the policy. The model
// never writes any of it.
import { capabilityNote } from "./capabilities.ts";
import type { GroupStatus } from "./group.ts";
import type { AgentPolicy } from "./policy.ts";

export interface GuidanceLike {
  id: string;
  guidance: string;
}

export function assembleBriefing(opts: {
  system: string;
  defs: readonly GuidanceLike[];
  statuses: readonly GroupStatus[];
  policy: AgentPolicy;
}): string {
  const fill = (text: string) =>
    text
      .replaceAll("{{operator}}", opts.policy.operator)
      .replaceAll("{{agent}}", opts.policy.agent)
      .replaceAll("{{maxKeyLimitUsd}}", String(opts.policy.maxKeyLimitUsd));
  const byId = new Map(opts.defs.map((d) => [d.id, d]));
  const parts = [fill(opts.system)];
  for (const status of opts.statuses) {
    if (!status.enabled) continue;
    const def = byId.get(status.id);
    if (def?.guidance) parts.push(fill(def.guidance));
  }
  parts.push(capabilityNote(opts.statuses, opts.policy.operator));
  return parts.join("\n\n");
}
