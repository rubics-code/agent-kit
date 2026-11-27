# @rubics-code/agent-kit

The machinery for an agent: tool groups, the approval gate, secret delivery, key loading (env, Infisical Agent Vault, Infisical identity), the agent loop, a terminal chat, a local web chat, and a container mode. An agent gives data; the Kit does the rest.

See [ADR-0004](../../docs/adr/0004-kit-is-a-public-package.md) and the [plan](../../docs/plans/agent-kit-extraction.md).

## An agent

```ts
import { readFileSync } from "node:fs";
import { type AgentConfig, cloudflareGroup, infisicalGroup, runAgent } from "@rubics-code/agent-kit";

export const SCOUT: AgentConfig = {
  name: "Scout",
  model: "deepseek/deepseek-v4-flash-0731",
  groups: [cloudflareGroup, infisicalGroup],
  system: readFileSync(new URL("../home/prompts/system.md", import.meta.url), "utf8"),
  policy: { agent: "Scout", operator: "Greg", maxKeyLimitUsd: 20, infisical: { /* readablePath, writablePath, envLabel */ } },
  infisical: { url: "https://secrets.example.com", projectId: "…", path: "/scout", env: () => "prod", tokenKey: "SCOUT_INFISICAL_TOKEN" },
};

await runAgent(SCOUT);
```

`agents/ruby` is the full example.

## A tool group

A group is one `ToolGroupDef` object: an id, a name, the service host (for Agent Vault), the env keys, a setup hint, the briefing guidance, `connect` (keys to client), and `tools` (client to tools). Make each write tool with `approvedTool`, so the operator must approve it. Add the group to the agent's `groups` list. A group can live in the agent's own repo.

## Env variables

`<PREFIX>` is the agent's name in upper case, or `envPrefix`.

| Variable | Use |
| --- | --- |
| `<PREFIX>_MODEL` | Replaces the pinned model, for a test. |
| `<PREFIX>_WEB_PORT` | The web chat port. Default 4141. |
| `<PREFIX>_KEYS=user` | Skips the identity file and uses your own keys. |
| `<PREFIX>_HEADLESS=1` | Container mode: no terminal, a health server, and new secrets go to Infisical. |
| `<PREFIX>_HEALTH_PORT` | The health port in container mode. Default 8080. |

## Container mode

The web chat stays on `127.0.0.1` inside the container. Reach it with `kubectl port-forward`. `GET /healthz` answers on all addresses. A new secret with no `storeAt` goes to the Infisical folder `<infisical.path>/inbox` (or `secretInbox`). `agents/ruby/Dockerfile` and `agents/ruby/entrypoint.sh` show the image and the Agent Vault start.

## Build and checks

| Command | What it does |
| --- | --- |
| `pnpm --filter @rubics-code/agent-kit test` | The Kit's tests. `test/fixture.ts` is a test agent built like Ruby. |
| `pnpm --filter @rubics-code/agent-kit build` | Compiles `src/` to `dist/` and copies the `.md` and `.html` files. |
| `pnpm --filter @rubics-code/agent-kit pack-check` | Builds, packs, installs the tarball in an empty folder, runs it, and type-checks a consumer file. |

In this workspace, `exports` points at `src/`, so an agent needs no build. `publishConfig.exports` points at `dist/` for the published package, because Node does not strip types from files under `node_modules`.
