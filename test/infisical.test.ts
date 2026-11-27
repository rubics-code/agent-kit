import { AIMessage, fakeModel } from "langchain";
import { Command } from "@langchain/langgraph";
import { describe, expect, it } from "vitest";
import { createRuby } from "./fixture.ts";
import { assemble } from "./fixture.ts";
import { type ApprovalRequest, isApprovalRequest } from "../src/index.ts";
import { loadIdentityCredentials } from "./fixture.ts";
import { HttpInfisical, type SecretPlace } from "../src/index.ts";
import { MemoryInfisical } from "../src/index.ts";
import { MemoryOpenRouterKeys } from "../src/index.ts";

function setup(model: ReturnType<typeof fakeModel>, store: MemoryInfisical | null = seeded()) {
  const delivered: { label: string; value: string }[] = [];
  const keys = new MemoryOpenRouterKeys([{ name: "Ruby", limit: 20, usageMonthly: 3 }]);
  const { tools, briefing } = assemble({
    services: {
      openrouter: keys,
      ...(store ? { infisical: store } : {}),
    },
    secrets: { deliver: async (label: string, value: string) => void delivered.push({ label, value }) },
    enable: { has: () => true },
  });
  const ruby = createRuby({ model, tools, briefing });
  const config = { configurable: { thread_id: crypto.randomUUID() } };
  return { store, keys, delivered, ruby, config };
}

function seeded() {
  return new MemoryInfisical({
    "/ruby": { OPENROUTER_API_KEY: "sk-ruby-secret" },
    "/agents/lyra": { OPENROUTER_API_KEY: "sk-lyra-secret", API_SERVER_KEY: "lyra-api-secret" },
    "/agents/tally": { API_SERVER_KEY: "tally-api-secret" },
    "/argocd": { ADMIN: "argo-secret" },
  });
}

function approvals(state: { __interrupt__?: { id?: string; value?: unknown }[] }) {
  return (state.__interrupt__ ?? []).filter((i) => isApprovalRequest(i.value)) as {
    id: string;
    value: ApprovalRequest;
  }[];
}

const text = (state: { messages: { content: unknown }[] }) => JSON.stringify(state.messages.map((m) => m.content));
const SECRETS = ["sk-ruby-secret", "sk-lyra-secret", "lyra-api-secret", "tally-api-secret", "argo-secret"];

describe("Infisical tools", () => {
  it("lists folders and names with no approval and no values", async () => {
    const model = fakeModel()
      .respondWithTools([
        { name: "infisical_list_folders", args: { path: "/agents" }, id: "f" },
        { name: "infisical_list_secret_names", args: { path: "/agents/lyra" }, id: "n" },
      ])
      .respond(new AIMessage("ok"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "what is there?" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(text(state)).toContain("lyra");
    expect(text(state)).toContain("API_SERVER_KEY");
    for (const s of SECRETS) expect(text(state)).not.toContain(s);
  });

  it("refuses a folder outside /ruby and /agents before any call", async () => {
    const model = fakeModel()
      .respondWithTools([{ name: "infisical_list_secret_names", args: { path: "/argocd" }, id: "n" }])
      .respond(new AIMessage("no"));
    const { ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "argo?" }] }, config);

    expect(text(state)).not.toContain("ADMIN");
  });

  it("makes a random secret after approval, and the value never enters the conversation", async () => {
    const call = { name: "infisical_create_random_secret", args: { path: "/agents/nova", name: "API_SERVER_KEY" }, id: "r" };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("done"));
    const { store, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "key for nova" }] }, config);
    const [req] = approvals(first);
    expect(req?.value.summary).toBe(
      "Make a random secret (32 bytes, hex) and store it in Infisical staging /agents/nova/API_SERVER_KEY (new, makes the folder /agents/nova)",
    );
    expect(store!.writes).toHaveLength(0);

    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    const value = store!.folders.get("/agents/nova")!.get("API_SERVER_KEY")!;
    expect(value).toMatch(/^[0-9a-f]{64}$/);
    expect(text(state)).not.toContain(value);
  });

  it("does not replace a secret with a random one", async () => {
    const call = { name: "infisical_create_random_secret", args: { path: "/agents/lyra", name: "API_SERVER_KEY" }, id: "r" };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("ok"));
    const { store, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(store!.writes).toHaveLength(0);
    expect(store!.folders.get("/agents/lyra")!.get("API_SERVER_KEY")).toBe("lyra-api-secret");
  });

  it("refuses to write in /agents itself", async () => {
    const call = { name: "infisical_create_random_secret", args: { path: "/agents", name: "X" }, id: "r" };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("no"));
    const { store, ruby, config } = setup(model);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(store!.writes).toHaveLength(0);
  });

  it("stores a new OpenRouter key in Infisical, and says when it replaces a value", async () => {
    const call = {
      name: "openrouter_create_key",
      args: { name: "Lyra", limitUsd: 10, storeAt: { path: "/agents/lyra", name: "OPENROUTER_API_KEY" } },
      id: "k",
    };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("done"));
    const { store, keys, delivered, ruby, config } = setup(model);

    const first = await ruby.invoke({ messages: [{ role: "user", content: "new key for lyra" }] }, config);
    const [req] = approvals(first);
    expect(req?.value.summary).toBe(
      'Create the OpenRouter API key "Lyra" (limit $10.00 monthly with no expiry) and store it in Infisical staging /agents/lyra/OPENROUTER_API_KEY (REPLACES the current value)',
    );

    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    const stored = store!.folders.get("/agents/lyra")!.get("OPENROUTER_API_KEY")!;
    expect(stored).not.toBe("sk-lyra-secret");
    expect([...keys.values.values()]).toContain(stored);
    expect(delivered).toHaveLength(0);
    expect(text(state)).not.toContain(stored);
    expect(text(state)).toContain("stored in Infisical staging /agents/lyra/OPENROUTER_API_KEY");
  });

  it("sends the secret to the terminal when Infisical fails after the key exists", async () => {
    class Broken extends MemoryInfisical {
      override async put(_p: SecretPlace, _v: string): Promise<void> {
        throw new Error("Infisical 403: no permission");
      }
    }
    const call = {
      name: "openrouter_create_key",
      args: { name: "Nova", limitUsd: 5, storeAt: { path: "/agents/nova", name: "OPENROUTER_API_KEY" } },
      id: "k",
    };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("done"));
    const { delivered, ruby, config } = setup(model, new Broken());

    const first = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);
    const [req] = approvals(first);
    const state = await ruby.invoke(
      new Command({ resume: { [req!.id]: { approved: true, argsHash: req!.value.argsHash } } }),
      config,
    );

    expect(delivered).toHaveLength(1);
    expect(text(state)).toContain("went to Greg's terminal instead");
    expect(text(state)).not.toContain(delivered[0]!.value);
  });

  it("refuses storeAt when Infisical is not set up, before any approval", async () => {
    const call = {
      name: "openrouter_create_key",
      args: { name: "Lyra", limitUsd: 10, storeAt: { path: "/agents/lyra", name: "OPENROUTER_API_KEY" } },
      id: "k",
    };
    const model = fakeModel().respondWithTools([call]).respond(new AIMessage("ok"));
    const { keys, ruby, config } = setup(model, null);

    const state = await ruby.invoke({ messages: [{ role: "user", content: "x" }] }, config);

    expect(approvals(state)).toHaveLength(0);
    expect(keys.writes).toHaveLength(0);
    expect(text(state)).toContain("Infisical is not set up");
  });
});

describe("HttpInfisical", () => {
  function fakeFetch(routes: (method: string, url: URL, body: unknown) => { status?: number; json: unknown }) {
    const calls: { method: string; url: URL; body: unknown; auth: string | undefined }[] = [];
    const f = (async (input: string | URL | Request, init?: RequestInit) => {
      const url = new URL(String(input));
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, url, body, auth: (init?.headers as Record<string, string> | undefined)?.Authorization });
      const r = routes(method, url, body);
      return new Response(JSON.stringify(r.json), { status: r.status ?? 200 });
    }) as typeof fetch;
    return { f, calls };
  }
  const opts = (f: typeof fetch) => ({ baseUrl: "https://inf.test", projectId: "p1", env: "prod", fetch: f });
  const login = { json: { accessToken: "tok", expiresIn: 3600 } };

  it("logs in once, and logs in again after a 401", async () => {
    let first = true;
    const { f, calls } = fakeFetch((m, url) => {
      if (url.pathname.endsWith("/login")) return login;
      if (first) {
        first = false;
        return { status: 401, json: { message: "expired" } };
      }
      return { json: { folders: [{ name: "lyra" }, { name: "tally" }] } };
    });
    const inf = new HttpInfisical({ clientId: "id", clientSecret: "s" }, opts(f));

    expect(await inf.listFolders("/agents")).toEqual(["lyra", "tally"]);
    expect(calls.filter((c) => c.url.pathname.endsWith("/login"))).toHaveLength(2);
    expect(calls.at(-1)!.auth).toBe("Bearer tok");
    expect(calls.at(-1)!.url.searchParams.get("workspaceId")).toBe("p1");
  });

  it("asks for names without values", async () => {
    const { f, calls } = fakeFetch((_m, url) =>
      url.pathname.endsWith("/login") ? login : { json: { secrets: [{ secretKey: "B" }, { secretKey: "A" }] } },
    );
    const names = await new HttpInfisical({ clientId: "id", clientSecret: "s" }, opts(f)).listSecretNames("/ruby");
    expect(names).toEqual(["A", "B"]);
    expect(calls.at(-1)!.url.searchParams.get("viewSecretValue")).toBe("false");
  });

  it("makes the missing folders, then creates the secret", async () => {
    const { f, calls } = fakeFetch((m, url) => {
      if (url.pathname.endsWith("/login")) return login;
      if (url.pathname === "/api/v1/folders" && m === "GET") {
        return { json: { folders: url.searchParams.get("path") === "/" ? [{ name: "agents" }] : [] } };
      }
      if (url.pathname === "/api/v3/secrets/raw" && m === "GET") return { json: { secrets: [] } };
      return { json: {} };
    });
    await new HttpInfisical({ clientId: "id", clientSecret: "s" }, opts(f)).put(
      { path: "/agents/nova", name: "API_SERVER_KEY" },
      "v",
    );

    const writes = calls.filter((c) => c.method !== "GET" && !c.url.pathname.endsWith("/login"));
    expect(writes.map((c) => `${c.method} ${c.url.pathname}`)).toEqual([
      "POST /api/v1/folders",
      "POST /api/v3/secrets/raw/API_SERVER_KEY",
    ]);
    expect(writes[0]!.body).toEqual({ workspaceId: "p1", environment: "prod", name: "nova", path: "/agents" });
    expect(writes[1]!.body).toMatchObject({ secretPath: "/agents/nova", secretValue: "v", type: "shared" });
  });

  it("replaces a secret that exists with PATCH", async () => {
    const { f, calls } = fakeFetch((m, url) => {
      if (url.pathname.endsWith("/login")) return login;
      if (url.pathname === "/api/v1/folders") return { json: { folders: [{ name: "ruby" }] } };
      if (url.pathname === "/api/v3/secrets/raw" && m === "GET") return { json: { secrets: [{ secretKey: "K" }] } };
      return { json: {} };
    });
    await new HttpInfisical({ clientId: "id", clientSecret: "s" }, opts(f)).put({ path: "/ruby", name: "K" }, "v");
    expect(calls.at(-1)!.method).toBe("PATCH");
  });
});

describe("HttpInfisical with an environment per folder", () => {
  it("uses staging for /agents, also to check the folder in /, and prod for /ruby", async () => {
    const seen: { method: string; path: string; env: string | null }[] = [];
    const f = (async (u: string | URL | Request, init?: RequestInit) => {
      const url = new URL(String(u));
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      seen.push({ method: init?.method ?? "GET", path: url.pathname, env: url.searchParams.get("environment") ?? body?.environment ?? null });
      if (url.pathname === "/api/v1/folders") return Response.json({ folders: [{ name: "agents" }, { name: "ruby" }] });
      return Response.json({ secrets: [] });
    }) as typeof fetch;
    const { infisicalEnv } = await import("./fixture.ts");
    const inf = new HttpInfisical({ token: "t" }, { baseUrl: "https://inf.test", projectId: "p", env: infisicalEnv, fetch: f });

    await inf.put({ path: "/agents/nova", name: "K" }, "v");
    await inf.listSecretNames("/ruby");

    const envs = seen.map((c) => c.env);
    expect(envs.slice(0, -1).every((e) => e === "staging")).toBe(true);
    expect(envs.at(-1)).toBe("prod");
    expect(seen.find((c) => c.method === "POST" && c.path === "/api/v1/folders")?.env).toBe("staging");
  });
});

describe("loadIdentityCredentials", () => {
  it("reads Ruby's keys from /ruby and removes the client secret from the environment", async () => {
    const f = (async (input: string | URL | Request) => {
      const url = new URL(String(input));
      if (url.pathname.endsWith("/login")) return Response.json({ accessToken: "tok", expiresIn: 3600 });
      expect(url.searchParams.get("secretPath")).toBe("/ruby");
      return Response.json({ secrets: [{ secretKey: "OPENROUTER_API_KEY", secretValue: "sk-x" }] });
    }) as typeof fetch;
    const env: NodeJS.ProcessEnv = { INFISICAL_CLIENT_ID: "id", INFISICAL_CLIENT_SECRET: "s" };

    const { creds } = await loadIdentityCredentials(env, { fetch: f });

    expect(creds.mode).toBe("identity");
    expect(creds.get("OPENROUTER_API_KEY")).toBe("sk-x");
    expect(creds.optional("MXROUTE_API_KEY")).toBeUndefined();
    expect(env.INFISICAL_CLIENT_SECRET).toBeUndefined();
  });
});

describe("readIdentityFile", () => {
  it("takes the values from the file, not from the shell", async () => {
    const { mkdtempSync, writeFileSync } = await import("node:fs");
    const { join } = await import("node:path");
    const { tmpdir } = await import("node:os");
    const { readIdentityFile } = await import("./fixture.ts");
    const file = join(mkdtempSync(join(tmpdir(), "ruby-")), ".env");
    writeFileSync(file, "INFISICAL_CLIENT_ID=from-file\nINFISICAL_CLIENT_SECRET=s\n");
    const before = process.env.INFISICAL_CLIENT_ID;
    process.env.INFISICAL_CLIENT_ID = "from-shell";
    try {
      expect(readIdentityFile(file)).toEqual({ INFISICAL_CLIENT_ID: "from-file", INFISICAL_CLIENT_SECRET: "s" });
    } finally {
      if (before === undefined) delete process.env.INFISICAL_CLIENT_ID;
      else process.env.INFISICAL_CLIENT_ID = before;
    }
  });

  it("is undefined when there is no file", async () => {
    const { readIdentityFile } = await import("./fixture.ts");
    expect(readIdentityFile("/nonexistent/ruby/.env")).toBeUndefined();
  });
});
