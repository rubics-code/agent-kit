// Where a delivered secret goes. It never goes into the conversation, so the
// model and the traces never hold it. The CLI prints it; the deployed agent
// writes it to Infisical.
import type { z } from "zod";

// The Infisical paths an agent may read and write, as schemas, and how a path
// is labelled in copy (which environment it belongs to). Policy, not identity:
// each agent brings its own (ADR-0003).
export interface InfisicalPolicy {
  readablePath: z.ZodType<string>;
  writablePath: z.ZodType<string>;
  envLabel(path: string): string;
}

// The agent's identity and limits, passed as data into the Kit's machinery.
// The Kit writes the approval and rejection copy from it; the model never
// writes copy. Config (URLs, ports, model IDs) stays with the agent.
export interface AgentPolicy {
  agent: string;
  operator: string;
  maxKeyLimitUsd: number;
  infisical: InfisicalPolicy;
}
