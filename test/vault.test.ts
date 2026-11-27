import { describe, expect, it } from "vitest";
import { reviewGroup, type EnableEnv } from "../src/index.ts";
import { GROUPS } from "./fixture.ts";
import { loadCredentials } from "./fixture.ts";
import { HttpInfisical } from "../src/index.ts";
import { buildRuby } from "./fixture.ts";
import { sessionHosts } from "./fixture.ts";

const services = (hosts: string[][], status = "ready") =>
  Response.json({ session: { accessBundle: "ruby" }, services: hosts.map((h, i) => ({ name: `s${i}`, hosts: h, status })) });

describe("sessionHosts", () => {
  it("asks the Infisical API from INFISICAL_AGENT_VAULT_URL, with or without /api", async () => {
    const urls: string[] = [];
    const f = (async (u: string | URL | Request) => (urls.push(String(u)), services([["openrouter.ai"], ["API.MXROUTE.COM:443"]]))) as typeof fetch;

    const hosts = await sessionHosts({ INFISICAL_AGENT_VAULT_URL: "https://secrets.example.com/api/" }, f);

    expect(urls).toEqual(["https://secrets.example.com/api/v1/agent-vault/session/services"]);
    expect([...hosts!].sort()).toEqual(["api.mxroute.com", "openrouter.ai"]);
  });

  it("sends the session token from HTTPS_PROXY as a Bearer token", async () => {
    let auth: string | undefined;
    const f = (async (_u: string | URL | Request, init?: RequestInit) => {
      auth = (init?.headers as Record<string, string>).Authorization;
      return services([["openrouter.ai"]]);
    }) as typeof fetch;
    await sessionHosts({ HTTPS_PROXY: "http://x-agent-vault:agv_abc%2F1@100.64.0.1:17323" }, f);
    expect(auth).toBe("Bearer agv_abc/1");
  });

  it("leaves out a service that is not ready", async () => {
    const f = (async () => services([["api.cloudflare.com"]], "pending")) as typeof fetch;
    expect([...(await sessionHosts({}, f))!]).toEqual([]);
  });

  it("is undefined when Infisical does not answer", async () => {
    expect(await sessionHosts({}, (async () => new Response("no", { status: 404 })) as typeof fetch)).toBeUndefined();
    expect(await sessionHosts({}, (async () => { throw new Error("down"); }) as typeof fetch)).toBeUndefined();
  });
});

describe("tool groups in Agent Vault mode", () => {
  const infisicalHost = new URL("https://secrets.rubicscode.com").host;
  const hosts = new Set(["openrouter.ai", infisicalHost]);

  it("turn on only the groups whose host is in the session, and name the missing host", () => {
    const statuses = GROUPS.map((def) =>
      reviewGroup(def, { has: () => true, hasStore: true, vaultHosts: hosts } as EnableEnv, {
        host: def.id === "infisical" ? infisicalHost : undefined,
      }),
    );
    const by = Object.fromEntries(statuses.map((g) => [g.id, g]));

    expect(by.openrouter!.enabled).toBe(true);
    expect(by.infisical!.enabled).toBe(true);
    expect(by.mxroute).toMatchObject({ enabled: false, missing: ["api.mxroute.com"] });
    expect(by.cloudflare!.setup).toContain("Agent Vault service for this host");
  });

  it("build Ruby without the tools of a missing service, although every placeholder exists", () => {
    const creds = loadCredentials({
      INFISICAL_AGENT_VAULT_SESSION_TOKEN: "s",
      HTTPS_PROXY: "http://localhost:17323",
      NODE_USE_ENV_PROXY: "1",
    });
    const { groups, tools } = buildRuby(creds, { deliver: async () => {} }, { vaultHosts: new Set(["openrouter.ai"]) });
    expect(groups.filter((g) => g.enabled).map((g) => g.name)).toEqual(["OpenRouter keys"]);
    expect(tools).toEqual(["openrouter_get_credits", "openrouter_list_keys", "openrouter_create_key", "openrouter_update_key"]);
  });
});

describe("HttpInfisical with a static token", () => {
  it("sends the token as it is and never logs in", async () => {
    const calls: { path: string; auth: string | undefined }[] = [];
    const f = (async (u: string | URL | Request, init?: RequestInit) => {
      calls.push({ path: new URL(String(u)).pathname, auth: (init?.headers as Record<string, string>).Authorization });
      return Response.json({ folders: [{ name: "lyra" }] });
    }) as typeof fetch;
    const inf = new HttpInfisical({ token: "__RUBY_INFISICAL_TOKEN__" }, { baseUrl: "https://inf.test", projectId: "p", env: "prod", fetch: f });

    expect(await inf.listFolders("/agents")).toEqual(["lyra"]);
    expect(calls).toEqual([{ path: "/api/v1/folders", auth: "Bearer __RUBY_INFISICAL_TOKEN__" }]);
  });
});
