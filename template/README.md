# {{Name}}

An agent on [`@rubics-code/agent-kit`](https://github.com/rubics-code/agent-kit). Node 24 runs the TypeScript directly, with no build step.

## Run it on a laptop

```bash
npm install
OPENROUTER_API_KEY=... npm run chat
```

The terminal chat starts, and a web chat opens on http://localhost:4141. Each write tool stops and asks you to approve the exact action.

## Files

| Path | What it holds |
| --- | --- |
| `src/agent.ts` | {{Name}} as data: the model, the tool groups, the policy, and the Infisical config. |
| `src/main.ts` | Starts {{Name}}. |
| `src/groups/notes.ts` | An example tool group. Replace it with your own groups. |
| `home/prompts/system.md` | The system prompt. |
| `home/memory/` | The writable part of the home. Git ignores it. |
| `test/` | Fast tests with no model. `npm test`. |
| `evals/` | The real model against in-memory services. `OPENROUTER_API_KEY=... npm run eval`. |

## Add a tool group

Add a Kit group to `GROUPS` in `src/agent.ts`, for example `cloudflareGroup`. The group turns on when its keys are set, for example `CLOUDFLARE_API_TOKEN`. Write your own group like `src/groups/notes.ts`, and make each write tool with `approvedTool`.

## Keys

The Kit gets the keys in one of three modes:

- **env:** the keys are env variables.
- **Agent Vault:** run {{Name}} under `infisical agent-vault run`. The process holds only placeholders, and the proxy adds the real keys.
- **Infisical identity:** set `infisical` and `identityFile` in `src/agent.ts`. The `.env` file holds the machine identity's client ID and secret.

## Container mode

```bash
npm install          # makes package-lock.json, which the image needs
docker build -t {{name}} .
docker run -e OPENROUTER_API_KEY=... -p 8080:8080 {{name}}
```

`{{NAME}}_HEADLESS=1` is set in the image: no terminal, a health route on `:8080/healthz`, and the web chat on `127.0.0.1:4141` inside the container (use `kubectl port-forward`). To run behind Agent Vault, set `INFISICAL_AGENT_VAULT_PROXY_ADDRESS` and the other `INFISICAL_*` variables. `entrypoint.sh` lists them.

## Env variables

| Variable | Use |
| --- | --- |
| `{{NAME}}_MODEL` | Replaces the pinned model, for a test. |
| `{{NAME}}_WEB_PORT` | The web chat port. Default 4141. |
| `{{NAME}}_KEYS=user` | Skips the identity file and uses your own keys. |
| `{{NAME}}_HEADLESS=1` | Container mode. |
