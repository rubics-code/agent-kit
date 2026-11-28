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

## The Go Kit and the Agent Console (in progress)

The Kit is moving to Go, on Google ADK Go v2. The Go code is in this repo, next to the TypeScript package, and npm does not publish it:

- `kit/`: the Kit packages. `approval` (the approval gate), `a2ui` (A2UI cards over A2A), `taskstore` (A2A tasks in SQL), `dashboard`, `transcribe` and `fakemodel`.
- `cmd/devagent`: the dev agent. It serves A2A, the ADK REST API, the Kit endpoints and the console.
- `web/`: the Agent Console, a React and Vite client that renders A2UI. The Go binary serves the built console with `go:embed`.

The console holds no key. It talks to an agent only over A2A and the Kit endpoints, and the agent keeps its own keys.

### An agent in Go

An agent is plain Go code that imports the Kit and calls `run.Run`. There is no `init` command and no template: add the tools one at a time, as you need them. `cmd/devagent` is an example.

```go
cfg := run.Config{
	Name:      "Example",
	Model:     "deepseek/deepseek-v4-flash-0731",
	Home:      "./home", // prompts/system.md, policy.json, memory/*.md
	Groups:    []group.Group{ /* tool groups */ },
	Tools:     func(k run.Kit) ([]tool.Tool, error) { /* the agent's own tools */ },
	CoreTools: []string{"list_notes", "write_note"},
}
log.Fatal(run.Run(context.Background(), cfg))
```

The tools are fixed at start. An agent never writes or changes its own tools at runtime: a tool changes only through a PR that a person reviews. Do not give an agent a toolset that loads tools at runtime (`mcptoolset`, `skilltoolset`), unless you pin it to a list that a person reviewed.

`run.Run` does this:

- It reads the keys in one of three modes: env, Agent Vault, or an Infisical identity (`IdentityFile`).
- It turns on the tool groups, puts each write tool behind the approval gate, and makes the briefing from the home.
- It keeps the instruction and the core tools fixed for the provider's cache. With `ToolSearch`, the other tools wait behind `search_tools`. A long thread is compacted.
- It serves A2A, the ADK REST API, the Kit endpoints (`/kit/info`, `/kit/dashboard`, `/kit/transcribe`) and the console.

| Variable | What it does |
| --- | --- |
| `<PREFIX>_HEADLESS=1` | Container mode: no terminal chat, `/healthz` on `<PREFIX>_HEALTH_ADDR` (default `:8080`), and a clean stop on SIGTERM. A new secret goes to the Infisical inbox folder, or is refused when there is no store. |
| `<PREFIX>_ADDR` | Where the console and A2A listen. Default `127.0.0.1:4141`. |
| `<PREFIX>_TOKEN` | The bearer token, when the address is not loopback. |
| `<PREFIX>_TRUSTED_HEADER` | A header that a trusted proxy sets with the user's name, when the address is not loopback. |
| `<PREFIX>_MODEL`, `<PREFIX>_DB` | Replace the model id and the SQLite file. |
| `<PREFIX>_KEYS=user` | Use the caller's own keys, not the identity file. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Sends traces, logs and metrics over OTLP. Each model call adds its tokens (input, output, cached, reasoning) to `gen_ai.client.token.usage`, and its cost to `kit.llm.cost`. |

`<PREFIX>` is the agent's name in upper case, or `Config.EnvPrefix`.

On a laptop, the terminal chat starts only when stdin is a terminal. A new secret goes to that terminal, never to the model or a client.

### Tool groups in Go

Each group is in `kit/groups`. Add a group to `Config.Groups`. A group is on when the agent holds its keys, or, in Agent Vault mode, when the session has a service for its host. Each group uses the Kit's HTTP client, so `HTTPS_PROXY` works. Each write tool is behind the approval gate, and code writes the approval text.

| Group | Package | Keys | Write tools |
| --- | --- | --- | --- |
| Cloudflare DNS | `kit/groups/cloudflare` | `CLOUDFLARE_API_TOKEN` | `cloudflare_create_dns_record` (MX and TXT only) |
| Cloudflare Registrar | `kit/groups/registrar` | `CLOUDFLARE_API_TOKEN` | `cloudflare_register_domain` (the approval holds the live price) |
| OpenRouter keys | `kit/groups/openrouter` | `OPENROUTER_MANAGEMENT_KEY` | `openrouter_create_key`, `openrouter_update_key` (the limit is at most `maxKeyLimitUsd`) |
| Infisical | `kit/groups/infisical` | the agent's secret store (`run.Config.Infisical`) | `infisical_create_random_secret` (in the policy's writable paths only) |
| MXroute | `kit/groups/mxroute` | `MXROUTE_SERVER`, `MXROUTE_USERNAME`, `MXROUTE_API_KEY` | `mxroute_add_domain`, `mxroute_create_mailbox`, `mxroute_reset_mailbox_password`, `mxroute_create_forwarder` |

A tool that makes a password or a key gives it to the secret sink, or to the secret store with `storeAt`. The model never sees the value.

### Evals and the console in another module

- `kit/fakes` has a fake of each service (Cloudflare, OpenRouter keys, MXroute, Infisical) as an `httptest` server. An agent's evals run the real model against these fakes, so an eval never changes a real account.
- `run.Config.HTTPClient`, `Store` and `Secrets` replace the outside client, the secret store and the secret sink, for an eval. `tokens.Transport.Observe` gets the provider's usage of each model call.
- An agent in another module gets the Kit with no console build. Build the console from the same commit (`npm ci && npm run build` in `web/`), and give the folder in `run.Config.ConsoleDir` or `<PREFIX>_CONSOLE_DIR`.

### Auth

On a loopback address, the agent has no auth. It refuses a browser request from another origin (unless the origin is in `Config.Origins`), a request for another host name (DNS rebinding), and a form post.

On any other address, the agent does not start without auth. It uses ADK's `server/authn`, and there are two choices:

- **A bearer token**: set `<PREFIX>_TOKEN` (32 characters or more). Each request needs `Authorization: Bearer <token>`. This is for programs, such as another agent or a script.
- **A trusted header**: set `<PREFIX>_TRUSTED_HEADER`, for example `X-Auth-Request-User`. Use it only behind a proxy that does the sign-in and sets that header. A person in a browser needs this, because the console cannot send a bearer token. The proxy must remove the header from the client's request, or a client could set it.

`/healthz` listens on its own address and needs no auth. The ADK REST API runs with no per-user rule (`authz`), so any caller that passes auth can act as any user. That is enough for one operator, and not for a team.

### Run the console on a laptop

You need Go 1.27 and Node 24.

1. Build the console:

   ```bash
   cd web && npm ci && npm run build && cd ..
   ```

2. Build the dev agent. The binary has the console in it:

   ```bash
   go build -o devagent ./cmd/devagent
   ```

3. Start the agent with the fake model. It needs no key and makes no provider calls:

   ```bash
   ./devagent -fake
   ```

4. Open `http://127.0.0.1:8080`. The agent that serves the page connects itself.

To use a real model, export `OPENROUTER_API_KEY` in the shell, then run `./devagent` with no `-fake`. Do not put the key in a file in the repo. The model is `deepseek/deepseek-v4-flash-0731` (`-model`), and hold-to-talk uses `google/gemini-3.5-flash-lite` (`-stt-model`). The data (sessions, tasks, notes, dashboard cards) is in `devagent.db` (`-db`).

To work on the console with live reload, start the agent, then run Vite. Vite sends `/a2a`, `/kit` and `/.well-known` to the agent:

```bash
./devagent -fake
```

```bash
cd web && npm run dev
```

Then open `http://127.0.0.1:5173`. To use an agent on another address, set `AGENT_URL` for Vite.

### Connect another agent

Use **Connect an agent** in the sidebar, and enter the agent's address. The agent must let the console's origin call it. For the dev agent, give the origins with `-origins`, for example `-origins http://127.0.0.1:8080`. The browser stores only the agent's name, address and A2A endpoint.

### Checks

| Command | What it does |
| --- | --- |
| `go test ./...` | The Kit packages, the dev agent and the embed handler. |
| `npm test` in `web/` | vitest for the client logic and the Kit catalog. |
| `npm run e2e` in `web/` | Playwright against the dev agent with the fake model, through Vite. |
| `npm run e2e:embed` in `web/` | Playwright against one binary with the embedded console. Run `npm run build` first. |

No test calls a model provider. Notes on the bundle size and the open `npm audit` findings are in `web/README.md`.

## License

MIT
