// The capability note for the briefing: which tool groups are on, and for the
// ones that are off, why. Derived from the group statuses, never hand-written.
import type { GroupStatus } from "./group.ts";

export function capabilityNote(groups: readonly GroupStatus[], operator: string): string {
  const lines = groups.map((g) =>
    g.enabled
      ? `- ${g.name}: on.`
      : `- ${g.name}: OFF. Missing: ${g.missing.join(", ")}. ${g.setup}`,
  );
  return `Your tool groups now. If ${operator} asks why a tool is not there, give this reason:\n${lines.join("\n")}`;
}
