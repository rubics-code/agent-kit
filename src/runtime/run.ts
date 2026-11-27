// An agent as data, and the one function that runs it: keys, tool groups,
// the model, the terminal chat and the local web chat.
import { randomUUID } from "node:crypto";
import { createInterface } from "node:readline/promises";
import { stderr, stdin, stdout } from "node:process";
import type { BaseChatModel } from "@langchain/core/language_models/chat_models";
import type { ApprovalDecision, ApprovalRequest } from "../approval.ts";
import { type AnyGroup, assembleAgent, connectServices } from "../assemble.ts";
import {
  type Credentials,
  type InfisicalConfig,
  isAgentVault,
  loadCredentials,
  loadIdentityCredentials,
  readIdentityFile,
  sessionHosts,
} from "../credentials.ts";
import { HttpInfisical } from "../groups/infisical/client.ts";
import type { AgentPolicy } from "../policy.ts";
import type { SecretStore } from "../secret-store.ts";
import type { SecretSink } from "../secrets.ts";
import { createAgentGraph, openRouterModel } from "./agent.ts";
import { inboxSink, noSink, startHealth } from "./container.ts";
import { openRouterTranscriber } from "./transcribe.ts";
import { resume, say } from "./turns.ts";
import { startWeb } from "./web/server.ts";

export interface AgentConfig {
  // The agent's name, in the copy and the web chat.
  name: string;
  // The model ID on OpenRouter. Pin it in code, so that a change is a PR.
  model: string;
  groups: readonly AnyGroup[];
  // The system prompt, usually read from the agent's home/prompts/system.md.
  system: string;
  policy: AgentPolicy;
  // The agent's Infisical project. `tokenKey` names the key that holds a
  // Token Auth token in Agent Vault mode. Without it, the Infisical group stays off.
  infisical?: InfisicalConfig & { tokenKey: string };
  // The .env file with the agent's Infisical machine identity (identity mode).
  identityFile?: URL | string;
  // The key that holds the model's API key. Default OPENROUTER_API_KEY.
  modelKey?: string;
  transcribeModel?: string;
  toolCallLimit?: number;
  // The prefix of the agent's own env variables: <PREFIX>_MODEL,
  // <PREFIX>_WEB_PORT and <PREFIX>_KEYS. Default: the name in upper case.
  envPrefix?: string;
  // The web chat port. Default 4141.
  port?: number;
  // Container mode: the health port (default 8080) and the Infisical folder
  // for a new secret that has no storeAt (default <infisical.path>/inbox).
  healthPort?: number;
  secretInbox?: string;
}

const prefixOf = (c: AgentConfig) => c.envPrefix ?? c.name.toUpperCase().replace(/[^A-Z0-9]+/g, "_");

export function modelId(config: AgentConfig, env: NodeJS.ProcessEnv = process.env): string {
  return env[`${prefixOf(config)}_MODEL`] ?? config.model;
}

// Builds the agent with the tool groups that are on. Only the model key is
// required. In Agent Vault mode each key is a placeholder, so `vaultHosts`
// (the hosts of the session's services) decides instead.
export function buildAgent(
  config: AgentConfig,
  creds: Credentials,
  secrets: SecretSink,
  opts: { infisical?: SecretStore | undefined; vaultHosts?: Set<string> | undefined; model?: BaseChatModel } = {},
) {
  const services = connectServices(config.groups, {
    holds: (k) => creds.holds(k),
    get: (k) => creds.get(k),
    store: opts.infisical,
  });
  const { tools, briefing, statuses } = assembleAgent({
    groups: config.groups,
    system: config.system,
    policy: config.policy,
    services,
    secrets,
    enable: {
      has: (k) => creds.holds(k),
      vaultHosts: opts.vaultHosts,
      hosts: config.infisical ? { infisical: new URL(config.infisical.url).host } : {},
    },
  });
  const model =
    opts.model ??
    openRouterModel({ apiKey: creds.get(config.modelKey ?? "OPENROUTER_API_KEY"), model: modelId(config), title: config.name });
  const graph = createAgentGraph({
    model,
    tools,
    briefing,
    ...(config.toolCallLimit ? { toolCallLimit: config.toolCallLimit } : {}),
  });
  return { graph, groups: statuses, tools: tools.map((t) => t.name) };
}

// Gets the keys in the mode that the environment shows. Agent Vault wins: in
// that mode the process holds no keys at all. <PREFIX>_KEYS=user skips the
// identity file and uses the caller's own keys.
export async function connectKeys(config: AgentConfig, env: NodeJS.ProcessEnv = process.env) {
  const idFile =
    isAgentVault(env) || env[`${prefixOf(config)}_KEYS`] === "user" || !config.identityFile || !config.infisical
      ? undefined
      : readIdentityFile(config.identityFile);
  const identity = idFile && config.infisical ? await loadIdentityCredentials(idFile, config.infisical) : undefined;
  const creds = identity?.creds ?? loadCredentials(env);

  // Agent Vault: the session's services decide the tool groups, and the
  // Infisical tools send a placeholder token that the proxy replaces.
  const vaultHosts = creds.mode === "agent-vault" && config.infisical ? await sessionHosts(config.infisical.url, env) : undefined;
  const infisical =
    identity?.infisical ??
    (creds.mode === "agent-vault" && config.infisical && (!vaultHosts || vaultHosts.has(new URL(config.infisical.url).host))
      ? new HttpInfisical(
          { token: creds.get(config.infisical.tokenKey) },
          { baseUrl: config.infisical.url, projectId: config.infisical.projectId, env: config.infisical.env },
        )
      : undefined);
  return { creds, vaultHosts, infisical };
}

// Local only: the secret goes to this terminal, never to the model or the browser.
const terminalSink: SecretSink = {
  async deliver(label, value) {
    stderr.write(`\n  New secret for ${label} (shown one time, save it now):\n  ${value}\n\n`);
  },
};

// Whether the agent runs in container mode: <PREFIX>_HEADLESS=1.
export function isHeadless(config: AgentConfig, env: NodeJS.ProcessEnv = process.env): boolean {
  return env[`${prefixOf(config)}_HEADLESS`] === "1";
}

// Runs the agent. On a laptop: the terminal chat and the local web chat. In
// container mode (<PREFIX>_HEADLESS=1): the web chat and a health server, until
// SIGTERM or SIGINT.
export async function runAgent(config: AgentConfig, env: NodeJS.ProcessEnv = process.env): Promise<void> {
  const { name } = config;
  const headless = isHeadless(config, env);

  const { creds, vaultHosts, infisical } = await connectKeys(config, env);
  if (creds.mode === "agent-vault" && config.infisical && !vaultHosts) {
    stderr.write("Agent Vault: Infisical did not list the session's services, so every tool group is on.\n");
  }
  const inbox = config.secretInbox ?? (config.infisical ? `${config.infisical.path}/inbox` : undefined);
  const sink = !headless ? terminalSink : infisical && inbox ? inboxSink(infisical, inbox) : noSink;
  if (headless && sink === noSink) {
    stderr.write("Container mode without Infisical: a new secret must go to Infisical with storeAt.\n");
  }
  const { graph, groups } = buildAgent(config, creds, sink, { infisical, vaultHosts });
  const model = modelId(config, env);

  const web = await startWeb(graph, {
    port: Number(env[`${prefixOf(config)}_WEB_PORT`] ?? config.port ?? 4141),
    info: { name, model, groups, keys: creds.mode },
    transcribe: openRouterTranscriber(creds.get(config.modelKey ?? "OPENROUTER_API_KEY"), {
      title: name,
      ...(config.transcribeModel ? { model: config.transcribeModel } : {}),
    }),
  });
  const banner = `${name} (${model}, keys: ${creds.mode}, groups: ${groups.filter((g) => g.enabled).map((g) => g.name).join(", ") || "none"}).\n`;

  if (headless) {
    const health = await startHealth(Number(env[`${prefixOf(config)}_HEALTH_PORT`] ?? config.healthPort ?? 8080));
    stdout.write(`${banner}Web chat on ${web.url} inside the container (use kubectl port-forward). Health: :${health.port}/healthz\n`);
    await new Promise<void>((resolve) => {
      process.once("SIGTERM", resolve);
      process.once("SIGINT", resolve);
    });
    web.server.close();
    health.server.close();
    return;
  }

  const rl = createInterface({ input: stdin, output: stdout });

  async function decide(req: ApprovalRequest): Promise<ApprovalDecision> {
    stdout.write(`\n  ${name} wants to: ${req.summary}\n`);
    const answer = (await rl.question("  Approve? Type yes to approve: ")).trim().toLowerCase();
    if (answer === "yes") return { approved: true, argsHash: req.argsHash };
    const reason = (await rl.question("  Reason (optional): ")).trim();
    return { approved: false, argsHash: req.argsHash, reason: reason || "rejected" };
  }

  const thread = randomUUID();

  async function turn(line: string): Promise<void> {
    let step = await say(graph, thread, line);
    while (step.kind === "approvals") {
      const decisions: Record<string, ApprovalDecision> = {};
      for (const item of step.items) decisions[item.id] = await decide(item);
      step = await resume(graph, thread, decisions);
    }
    stdout.write(`\n${name}: ${step.text}\n\n`);
  }

  stdout.write(
    banner +
      `Web chat: ${web.url}\n` +
      `Type here to chat in the terminal, or exit to stop.\n\n`,
  );
  for (;;) {
    const line = (await rl.question("You: ")).trim();
    if (!line) continue;
    if (line === "exit") break;
    try {
      await turn(line);
    } catch (err) {
      stderr.write(`\nError: ${err instanceof Error ? err.message : String(err)}\n\n`);
    }
  }
  rl.close();
  web.server.close();
}
