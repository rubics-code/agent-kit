// {{Name}} as data. main.ts, the tests and the evals all build from here.
import { readFileSync } from "node:fs";
import type { AgentConfig, AgentPolicy } from "@rubics-code/agent-kit";
import { z } from "zod";
import { notesGroup } from "./groups/notes.ts";

// Pin the model here, so that a model change is a PR and the evals run against it.
// {{NAME}}_MODEL replaces it for one run.
export const MODEL_ID = "deepseek/deepseek-v4-flash-0731";

// The tool groups. Adding a group is one entry in this list. The Kit has
// cloudflareGroup, registrarGroup, mxrouteGroup, openrouterGroup and
// infisicalGroup; each one turns on when its keys are set.
export const GROUPS = [notesGroup] as const;

// The Infisical folders that {{Name}} may read and write. The machine
// identity's Infisical role is the second limit.
const folder = z
  .string()
  .regex(/^\/{{name}}(\/[a-z0-9][a-z0-9_-]{0,63})*$/, "/{{name}}, or a folder under it")
  .describe("Infisical folder: /{{name}}, or a folder under it");

export const POLICY: AgentPolicy = {
  agent: "{{Name}}",
  operator: "{{operator}}",
  // The highest spend limit that {{Name}} can put on an OpenRouter key.
  maxKeyLimitUsd: 20,
  infisical: { readablePath: folder, writablePath: folder, envLabel: () => "prod" },
};

export const AGENT: AgentConfig = {
  name: "{{Name}}",
  model: MODEL_ID,
  groups: GROUPS,
  system: readFileSync(new URL("../home/prompts/system.md", import.meta.url), "utf8"),
  policy: POLICY,
  // Set the Infisical project to turn on the Infisical group, Agent Vault
  // mode and, in a container, the secret inbox. Neither value is a secret.
  // infisical: {
  //   url: "https://secrets.example.com",
  //   projectId: "…",
  //   path: "/{{name}}",
  //   env: () => "prod",
  //   tokenKey: "{{NAME}}_INFISICAL_TOKEN",
  // },
  // identityFile: new URL("../.env", import.meta.url),
};
