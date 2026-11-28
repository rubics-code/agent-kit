# @rubics-code/agent-kit

The machinery for an agent: tool groups, the approval gate, secret delivery, key loading (env, Infisical Agent Vault, Infisical identity), the agent loop, a terminal chat, a local web chat, and a container mode. An agent gives data; the Kit does the rest.

Each write tool goes through an approval gate: the operator sees a summary that code writes from the arguments, never the model, and must approve it. A new secret (a password, an API key) never goes into the conversation. It goes to the operator's terminal, or into Infisical.

## Install

Node 24 or later.

```bash
npm install @rubics-code/agent-kit
```

## Make a new agent

```bash
npx @rubics-code/agent-kit init scout
```

This makes `agents/scout/` with the agent as data (`src/agent.ts`), an example tool group with a write tool behind the approval gate, a system prompt, tests, evals, a Dockerfile, and an entrypoint for Agent Vault. `--dir` sets another folder, and `--operator` names who approves the actions (default: your git `user.name`). The agent is its own npm package with its own `package-lock.json`, which the Dockerfile needs.

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

Run it with `node src/main.ts`. Node 24 runs TypeScript directly.

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

The web chat stays on `127.0.0.1` inside the container. Reach it with `kubectl port-forward`. `GET /healthz` answers on all addresses. A new secret with no `storeAt` goes to the Infisical folder `<infisical.path>/inbox` (or `secretInbox`). In an image, start the agent behind Agent Vault with the Infisical CLI. The CLI mints a session with a machine identity, then removes the identity before the agent starts:

```sh
exec infisical agent-vault run --no-skills -- \
  env -u INFISICAL_UNIVERSAL_AUTH_CLIENT_ID -u INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET node src/main.ts
```

Set `INFISICAL_DOMAIN`, `INFISICAL_AGENT_VAULT_PROXY_ADDRESS`, `INFISICAL_AGENT_VAULT_PROJECT_ID`, `INFISICAL_AGENT_VAULT_ACCESS_BUNDLE`, `INFISICAL_UNIVERSAL_AUTH_CLIENT_ID` and `INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET`, and `NODE_USE_ENV_PROXY=1`. The `infisical/cli` image holds a static binary at `/bin/infisical` that you can copy into a `node:24-slim` image.

## Build and checks

| Command | What it does |
| --- | --- |
| `npm test` | The tests. `test/fixture.ts` is a test agent. |
| `npm run type-check` | Type-checks `src/` and `test/`. |
| `npm run build` | Compiles `src/` to `dist/` and copies the `.md` and `.html` files that the code reads. |
| `npm run pack-check` | Packs the package, installs the tarball in an empty folder, runs a consumer, type-checks a consumer file, and makes an agent with `init` that must pass its own type-check and tests. |

## Release

Make a GitHub release with a tag `v<major>.<minor>.<patch>`, for example `v0.2.0`. The `release` workflow sets that version, runs the tests, and publishes to npm. `version` in `package.json` stays `0.0.0`: the tag is the version. A release that is marked as a prerelease goes to the npm tag `next`.

The workflow uses trusted publishing (OIDC), so no npm token is stored, and npm adds provenance. The first release can use a short-lived `NPM_TOKEN` secret in the `npm` environment, because npm lets you add a trusted publisher only to a package that exists.

Until 1.0, a minor version can break the interface.

## License

MIT
