// How an agent gets its keys. Three modes:
// - env: `infisical run` puts the real values in the environment.
// - agent-vault: `infisical agent-vault run` starts the agent behind the Agent
//   Vault proxy. The agent sends placeholders such as __MXROUTE_API_KEY__, and
//   the proxy puts in the real values, so the process never holds a key. The
//   session's services (sessionHosts) decide which tool groups are on.
// - identity: the agent logs in with its Infisical machine identity and reads
//   its keys from its own Infisical folder.
// The Agent Vault services must use the placeholders that `placeholder` makes.
import { existsSync, readFileSync } from "node:fs";
import { parseEnv } from "node:util";
import { HttpInfisical } from "./groups/infisical/client.ts";

export interface Credentials<K extends string = string> {
  mode: "env" | "agent-vault" | "identity";
  get(name: K): string;
  optional(name: K): string | undefined;
  // Whether a key is held, for the tool-group check. Accepts any name so that
  // group definitions stay untyped about an agent's key list.
  holds(name: string): boolean;
}

// The agent's Infisical deployment, project, and own folder. `env` maps a
// folder to its Infisical environment.
export interface InfisicalConfig {
  url: string;
  projectId: string;
  path: string;
  env: (path: string) => string;
}

export const placeholder = (name: string) => `__${name}__`;

// `infisical agent-vault run` puts the session token into HTTPS_PROXY as the
// password of the user x-agent-vault. Some CLI versions also set
// INFISICAL_AGENT_VAULT_SESSION_TOKEN; 0.45.4 does not.
export function isAgentVault(env: NodeJS.ProcessEnv = process.env): boolean {
  if (env.INFISICAL_AGENT_VAULT_SESSION_TOKEN) return true;
  try {
    return new URL(env.HTTPS_PROXY ?? env.https_proxy ?? "").username === "x-agent-vault";
  } catch {
    return false;
  }
}

// Env mode or Agent Vault mode, from the environment.
export function loadCredentials<K extends string = string>(env: NodeJS.ProcessEnv = process.env): Credentials<K> {
  if (isAgentVault(env)) {
    if (!env.HTTPS_PROXY) throw new Error("Agent Vault mode: HTTPS_PROXY is not set");
    // Without it, Node's fetch ignores HTTPS_PROXY and sends the placeholders
    // directly to the API.
    if (env.NODE_USE_ENV_PROXY !== "1") throw new Error("Agent Vault mode: set NODE_USE_ENV_PROXY=1");
    const present = (name: K) => {
      if (env[name]) throw new Error(`Agent Vault mode: ${name} is in the environment. Remove it.`);
      return placeholder(name);
    };
    return { mode: "agent-vault", get: present, optional: present, holds: (name) => !env[name] };
  }

  return {
    mode: "env",
    get(name) {
      const value = env[name];
      if (!value) throw new Error(`${name} is not set`);
      return value;
    },
    optional: (name) => env[name] || undefined,
    holds: (name) => !!env[name],
  };
}

// The agent's identity file, read directly. A shell can export its own
// INFISICAL_CLIENT_ID (for example from a repo-root .env), and node
// --env-file never replaces a variable that is set, so the file must not go
// through the environment.
export function readIdentityFile(file: URL | string): NodeJS.ProcessEnv | undefined {
  if (!existsSync(file)) return undefined;
  const v = parseEnv(readFileSync(file, "utf8"));
  if (!v.INFISICAL_CLIENT_ID || !v.INFISICAL_CLIENT_SECRET) {
    throw new Error(`${file instanceof URL ? file.pathname : file} needs INFISICAL_CLIENT_ID and INFISICAL_CLIENT_SECRET`);
  }
  return { INFISICAL_CLIENT_ID: v.INFISICAL_CLIENT_ID, INFISICAL_CLIENT_SECRET: v.INFISICAL_CLIENT_SECRET };
}

// Identity mode. It removes the client secret from the given object after
// the client has it.
export async function loadIdentityCredentials<K extends string = string>(
  env: NodeJS.ProcessEnv,
  config: InfisicalConfig,
  opts: { fetch?: typeof fetch } = {},
): Promise<{ creds: Credentials<K>; infisical: HttpInfisical }> {
  const clientId = env.INFISICAL_CLIENT_ID;
  const clientSecret = env.INFISICAL_CLIENT_SECRET;
  if (!clientId || !clientSecret) throw new Error("Identity mode: INFISICAL_CLIENT_ID and INFISICAL_CLIENT_SECRET are not set");
  delete env.INFISICAL_CLIENT_SECRET;

  const infisical = new HttpInfisical(
    { clientId, clientSecret },
    {
      baseUrl: config.url,
      projectId: config.projectId,
      env: config.env,
      ...(opts.fetch ? { fetch: opts.fetch } : {}),
    },
  );
  const values = await infisical.readAll(config.path);
  return {
    infisical,
    creds: {
      mode: "identity",
      get(name) {
        const value = values[name];
        if (!value) throw new Error(`${name} is not in Infisical ${config.path}`);
        return value;
      },
      optional: (name) => values[name] || undefined,
      holds: (name) => !!values[name],
    },
  };
}

// Agent Vault: the hosts of the session's services, or undefined when
// Infisical does not answer (an older version, for example). Infisical wants
// the session token as a Bearer token.
interface SessionServices {
  services?: { name?: string; hosts?: string[]; status?: string }[];
}

export async function sessionHosts(
  infisicalUrl: string,
  env: NodeJS.ProcessEnv = process.env,
  f: typeof fetch = fetch,
): Promise<Set<string> | undefined> {
  const base = (env.INFISICAL_AGENT_VAULT_URL || infisicalUrl).replace(/\/+$/, "").replace(/\/api$/, "");
  const token = sessionToken(env);
  try {
    const res = await f(`${base}/api/v1/agent-vault/session/services`, {
      headers: { Accept: "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    });
    if (!res.ok) return undefined;
    const json = (await res.json()) as SessionServices;
    const hosts = (json.services ?? [])
      .filter((s) => !s.status || s.status === "ready")
      .flatMap((s) => s.hosts ?? [])
      .map((h) => h.toLowerCase().replace(/:443$/, ""));
    return new Set(hosts);
  } catch {
    return undefined;
  }
}

// The session token is the password in HTTPS_PROXY (user x-agent-vault), so
// the agent holds it already; it grants this session only.
export function sessionToken(env: NodeJS.ProcessEnv = process.env): string | undefined {
  if (env.INFISICAL_AGENT_VAULT_SESSION_TOKEN) return env.INFISICAL_AGENT_VAULT_SESSION_TOKEN;
  try {
    const url = new URL(env.HTTPS_PROXY ?? env.https_proxy ?? "");
    return url.username === "x-agent-vault" && url.password ? decodeURIComponent(url.password) : undefined;
  } catch {
    return undefined;
  }
}

// Every key that a group list can turn on, for a key list or a check.
export function groupKeys(groups: readonly { keys: readonly string[] }[]): string[] {
  return [...new Set(groups.flatMap((g) => g.keys))];
}
