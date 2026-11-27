// A test agent built like Ruby, so the Kit's tests check real copy and paths
// without a dependency on an agent package.
import type { BaseChatModel } from "@langchain/core/language_models/chat_models";
import type { StructuredTool } from "@langchain/core/tools";
import type { BaseCheckpointSaver } from "@langchain/langgraph";
import { z } from "zod";
import {
  type AgentConfig,
  type AgentPolicy,
  type Credentials,
  type Enable,
  type InfisicalConfig,
  type SecretSink,
  type SecretStore,
  type Services,
  assembleAgent,
  buildAgent,
  cloudflareGroup,
  createAgentGraph,
  infisicalGroup,
  loadCredentials as kitLoadCredentials,
  loadIdentityCredentials as kitLoadIdentity,
  mxrouteGroup,
  openRouterModel as kitModel,
  openrouterGroup,
  registrarGroup,
  sessionHosts as kitSessionHosts,
} from "../src/index.ts";

export { readIdentityFile } from "../src/index.ts";

export const INFISICAL_URL = "https://secrets.rubicscode.com";

export function infisicalEnv(path: string): string {
  return path === "/agents" || path.startsWith("/agents/") ? "staging" : "prod";
}

const SEGMENT = "[a-z0-9][a-z0-9_-]{0,63}";
const writablePath = z
  .string()
  .regex(new RegExp(`^/(ruby(/${SEGMENT})*|agents(/${SEGMENT})+)$`), "/ruby, or a folder under /agents such as /agents/lyra")
  .describe("Infisical folder: /ruby, or a folder under /agents such as /agents/lyra");
const readablePath = z
  .string()
  .regex(new RegExp(`^/(ruby|agents)(/${SEGMENT})*$`), "/ruby, /agents, or a folder under them")
  .describe("Infisical folder: /ruby, /agents, or a folder under them");

export const rubyPolicy: AgentPolicy = {
  agent: "Ruby",
  operator: "Greg",
  maxKeyLimitUsd: 100,
  infisical: { readablePath, writablePath, envLabel: infisicalEnv },
};

export const GROUPS = [mxrouteGroup, cloudflareGroup, registrarGroup, openrouterGroup, infisicalGroup] as const;
export const SYSTEM_PROMPT = "You are Ruby, a test agent. {{operator}} approves each change.";

export function assemble(opts: { services: Services; secrets: SecretSink; enable: Enable }) {
  return assembleAgent({ groups: GROUPS, system: SYSTEM_PROMPT, policy: rubyPolicy, ...opts });
}

export function createRuby(opts: {
  model: BaseChatModel;
  tools: StructuredTool[];
  briefing: string;
  checkpointer?: BaseCheckpointSaver;
}) {
  return createAgentGraph({ ...opts, toolCallLimit: 25 });
}

export const openRouterModel = (apiKey: string) => kitModel({ apiKey, model: "test/model", title: "Ruby" });

export const RUBY_INFISICAL: InfisicalConfig = { url: INFISICAL_URL, projectId: "test-project", path: "/ruby", env: infisicalEnv };

export const RUBY: AgentConfig = {
  name: "Ruby",
  model: "test/model",
  groups: GROUPS,
  system: SYSTEM_PROMPT,
  policy: rubyPolicy,
  infisical: { ...RUBY_INFISICAL, tokenKey: "RUBY_INFISICAL_TOKEN" },
};

export const loadCredentials = (env: NodeJS.ProcessEnv = process.env) => kitLoadCredentials(env);

export const loadIdentityCredentials = (env: NodeJS.ProcessEnv, opts: { fetch?: typeof fetch } = {}) =>
  kitLoadIdentity(env, RUBY_INFISICAL, opts);

export const sessionHosts = (env: NodeJS.ProcessEnv = process.env, f: typeof fetch = fetch) =>
  kitSessionHosts(INFISICAL_URL, env, f);

export function buildRuby(
  creds: Credentials,
  secrets: SecretSink,
  opts: { infisical?: SecretStore | undefined; vaultHosts?: Set<string> | undefined } = {},
) {
  const { graph, groups, tools } = buildAgent(RUBY, creds, secrets, opts);
  return { ruby: graph, groups, tools };
}
