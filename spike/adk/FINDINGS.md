# Phase 0 findings: the Go Kit on Google ADK Go v2

Date: 2026-11-28. ADK Go `google.golang.org/adk/v2` v2.8.0, a2a-go v2.7.0, `@a2ui/react` and `@a2ui/web_core` 0.16.1, Go 1.27.4.
Model: `deepseek/deepseek-v4-flash-0731` on OpenRouter, through `model/openaimodel` with the Chat Completions API.

The spike agent has one read tool (`list_notes`), one write tool (`write_note`) and 19 filler tools that do nothing. The filler tools give the agent 21 tool schemas, as Ruby has. Sessions and notes are in SQLite (`session/database`). The agent serves REST (`server/adkrest`) on `/api` and A2A JSON-RPC (`server/adka2a`) on `/a2a`. The raw responses and logs are in `evidence/`.

## Summary

| # | Question | Answer |
|---|----------|--------|
| 1 | Do `openaimodel` and OpenRouter work with tool calls on the model? | **Yes** |
| 2 | Confirmation shape over A2A and REST, and does it stay through a restart? | **Yes** over REST. **Partly** over A2A: the session stays, but the A2A task does not |
| 3 | Can code put its own summary and an args hash on a confirmation, and can an approval bind to the exact arguments? | **Yes** |
| 4 | Can a Go agent send A2UI v1.0 over A2A that `@a2ui/react` renders? | **Yes** |
| 5 | Does the model client use `HTTPS_PROXY`? | **Yes** |
| 6 | Tokens with `toolsearch` on and off, and does the provider cache a stable prefix? | **Yes**: about 4,960 tokens with all tools, 933 with search. The provider caches up to 98% of the prefix |

**Recommendation: build on ADK's loop.** Questions 2 and 3 pass, so the condition for our own loop in the plan does not apply. Keep the Kit's own interfaces (tool group, approval gate, secret sink) in front of ADK, as ADR-0005 says. The gaps below are small, and each one has a fix inside the Kit.

## 1. `openaimodel` and OpenRouter with tool calls: yes

- `openaimodel.NewModel(ctx, model, &ClientConfig{APIKey, BaseURL: "https://openrouter.ai/api/v1", API: openaimodel.APIChatCompletions})` works. The default API is the Responses API (`/v1/responses`). Set `APIChatCompletions` for OpenRouter.
- The model called `list_notes` and `write_note` with correct arguments (`evidence/rest/q1-read.json`, `q2-write.json`). It also used a gated tool after `search_tools` (`evidence/tokens/on-d.json`).
- ADK maps the usage into `genai.GenerateContentResponseUsageMetadata`: prompt, completion, cached (`prompt_tokens_details.cached_tokens`) and reasoning tokens. OpenRouter sent `provider: "Baidu"` for this model id.
- Caution: in 1 of 15 write requests, the model said "it's waiting for your approval" but did not call `write_note` (`evidence/web-usage.jsonl`, first turn). The task completed with no write and no card. Ruby's evals must catch this. The Kit can also check each reply: a reply that says "approval" with no pending confirmation is an error.

## 2. The shape of a confirmation, and a restart

The write tool calls `ctx.RequestConfirmation(hint, payload)`. ADK then stops the run. It does not call the model again until the person answers.

**REST** (`POST /api/run`, `evidence/rest/q2-write.json`). The run gives three events:

1. The model's `functionCall` `write_note` with the args.
2. A `functionResponse` from our tool (`{"status": "waiting for approval"}`), with `actions.requestedToolConfirmations[<call id>] = {hint, confirmed: false, payload}`.
3. A `functionCall` named `adk_request_confirmation`, with `longRunningToolIds: [<its id>]` and these args:
   `{"originalFunctionCall": {id, name, args}, "toolConfirmation": {hint, confirmed, payload}}`.

The client answers with a new message from the user:
`{"functionResponse": {"id": "<adk_request_confirmation id>", "name": "adk_request_confirmation", "response": {"confirmed": true, "payload": {...}}}}`.

**A2A** (`SendMessage`, `evidence/a2a/a2a-write.json`). The task goes to `TASK_STATE_INPUT_REQUIRED`. `status.message.parts` holds the same `adk_request_confirmation` call as a data part with `metadata: {"adk_type": "function_call", "adk_is_long_running": true}`. The model's `write_note` call and our tool's response go in an artifact. The client answers with a data part with `adk_type: "function_response"` (or with an A2UI action, see question 4). Note: a2a-go v2 serves A2A 1.0 only, so each request needs the header `A2A-Version: 1.0`.

**Restart.** The spike stopped the process while a confirmation waited, and started it again on the same SQLite file.

- REST: **yes**. The session still had the `adk_request_confirmation` event (`evidence/rest/q2-after-restart-session.json`). The answer after the restart resumed the tool.
- A2A: **partly**. A message with the old `taskId` fails with `TASK_NOT_FOUND`, because the a2a-go task store is in memory (`evidence/a2a/a2a-resume-taskid.json`). A new message in the same `contextId` works, because ADK uses the `contextId` as the session id. ADK found the pending confirmation in SQLite and wrote the note (`a2a-resume-context.json`).
- Fix for phase 2: give `a2asrv.NewHandler` a SQL task store (`a2asrv.WithTaskStore`, interface `taskstore.Store`). Until then, a client sends the answer with the `contextId` and no `taskId`.

## 3. The approval summary and the args hash: yes

- **Code writes the summary.** The tool makes `Approval{tool, summary, argsHash, args}` from its typed arguments and passes it as the `payload`, with the summary as the `hint`. The model never writes either field (`approval.go`, `tools.go`). Example: `Write the note "Groceries" (17 characters): "milk, eggs, bread"`.
- **The args hash.** `sha256` of the tool name and the canonical JSON of the args (sorted keys). The struct form and the map form give the same hash (`approval_test.go`).
- **The binding works.** When the tool runs again after the answer, it computes the hash again and compares it with the `argsHash` in the answer's payload.
  - A wrong hash: the tool refused, and nothing was written (`evidence/rest/q3-tampered.json`). This test ran after a restart.
  - The correct hash: the note was written (`q3-approved.json`).
- **A client cannot change the args.** On resume, ADK takes the original call from the agent's own event in the session (`originalFunctionCall`). It does not take it from the client's message. ADK also skips an answer whose original call appears twice with other args (`internal/llminternal/request_confirmation_processor.go`).
- **A replay does nothing.** The same approval sent a second time did not run the tool again. But ADK called the model once more (`q3-replay.json`). The Kit can drop a second answer before the runner, to save that call.
- Caution: the client sends back the hash, so the binding proves that the approver saw these args. It does not prove who the approver is. Use the REST `Authenticator` and `Authorizer` (`server/authn`, `server/authz`), or A2A auth, for that.
- For phase 2: put this logic in one Kit gate that wraps each write tool, so that a tool group gives only the summary function.

## 4. A2UI v1.0 over A2A to `@a2ui/react`: yes

- The agent card declares the extension `https://a2ui.org/a2a-extension/a2ui/v1.0` with the basic catalog (`evidence/a2a/a2a-agent-card.json`).
- The web client sends `metadata.a2uiRendererCapabilities = {"1.0": {"supportedCatalogIds": [<basic catalog>]}}` on each message, and the header `X-A2A-Extensions`.
- When the task is input-required and the client renders A2UI, `adka2a.ExecutorConfig.AfterExecuteCallback` adds one data part next to each confirmation. The part has `metadata: {"mimeType": "application/a2ui+json"}`, and its data is a list of v1.0 messages: `createSurface` and `updateComponents` (a Card with the summary, the args hash, and Approve and Reject buttons). The ADK function call part stays, so a client without A2UI still works.
- A button sends an A2UI `action` (`kit.approve` or `kit.reject`, with `confirmationId` and `argsHash` in the context). `adka2a.ExecutorConfig.A2APartConverter` turns it into the `adk_request_confirmation` function response. The client knows nothing about ADK.
- The React page in `web/` rendered the card with `@a2ui/react` and the v1 basic catalog. Approve wrote the note (`evidence/web-approval-card.jpg`, `evidence/web-after-approve.jpg`).
- Notes:
  - The npm packages are at 0.16.1, but they render spec v1.0 (`MessageProcessor` from `@a2ui/web_core`, `basicCatalog` from `@a2ui/web_core/catalogs/basic/v1`).
  - `npm audit` reports 4 high findings. All come from `zod` 3, which `@a2ui/*` requires (`^3.25.76`).
  - The page bundle is 730 kB before gzip.
  - The Go side has no A2UI library. The Kit builds the messages as plain maps (`a2ui.go`). That is enough for a small catalog of Kit components.

## 5. `HTTPS_PROXY`: yes

- With `HTTPS_PROXY=http://127.0.0.1:8899` and ADK's default HTTP client (`-raw-usage=false`), the local proxy (`cmd/proxy`) logged `CONNECT openrouter.ai:443`, and the agent answered (`evidence/proxy.log`).
- With `HTTPS_PROXY` set to a port that does not listen, the call failed with `proxyconnect tcp: ... connection refused` (`evidence/server-deadproxy.log`). So the client really uses the variable.
- The reason: openai-go uses Go's default transport, which reads `HTTPS_PROXY`. A Kit that gives its own `HTTPClient` must keep `Proxy: http.ProxyFromEnvironment`. Clone `http.DefaultTransport`, as `main.go` does.
- Not tested: Agent Vault itself. It inspects TLS, so the agent must trust its CA. Go on Linux reads `SSL_CERT_FILE`, which is enough in a container. On macOS, Go uses the system verifier, so the CA must be in the keychain, or the Kit sets `RootCAs`.

## 6. Tokens, tool search and the prompt cache

`scripts/tokens.sh` runs the same four turns with `-toolsearch=false` and `=true`. Raw usage: `evidence/tokens/usage.jsonl`. The instruction is short (about 120 tokens), so most of the prompt is tool schemas.

| | Tools in each request | Tool JSON | Prompt tokens, first call | Prompt tokens, second call | Prompt tokens for "What notes do I have?" (2 calls) |
|---|---|---|---|---|---|
| toolsearch off | 21 | 20,123 bytes | 4,958 | 5,003 | 9,961 |
| toolsearch on | 3 (`list_notes`, `write_note`, `search_tools`) | 2,871 bytes | 933 | 978 | 1,911 |

- The 19 hidden tools cost about 4,025 tokens on each call, about 210 tokens for each tool. Ruby's figure is about 3,500 tokens for 21 tools.
- A turn that needs a hidden tool costs one more model call (`search_tools`). After that, the tool's schema stays in the request, which added about 350 tokens. In the domain turn, the "on" run made 6 calls (8,337 prompt tokens), and the "off" run made 3 calls (15,109 prompt tokens). 3 of the 6 calls were the model's retries of a filler tool that does nothing, not a cost of the search.
- **Prefix cache: yes.** OpenRouter reports `prompt_tokens_details.cached_tokens`, in blocks of 256 tokens:
  - toolsearch off: 4,864 of 4,958 tokens cached (98%) on a warm prefix. One call had 0 cached, and one had 4,096.
  - toolsearch on: 256 cached on the first call, and 768 of 933 (82%) after that. The cache grew to 1,536 tokens in a long turn.
  - A call with 98% cached cost 1.1e-5 USD for its prompt. A call of the same size with 0 cached cost 2.3e-4 USD, about 20 times more (`cost_details` in the log).
- No `cache_write_tokens` were reported, and the spike sent no cache markers. The DeepSeek cache is automatic.
- To keep the prefix stable: keep the instruction and the core tools fixed, and put the variable parts (date, memory) after them. A tool that `search_tools` reveals changes the tool list. So the cached prefix stops at the tools, until the same set comes again.
- For phase 2: the token log in `usage.go` (a `BeforeModelCallback` and an `AfterModelCallback`) is a good start for the per-call token count in telemetry.

## Other notes for phase 2

- **Tools are fixed at build time.** Nothing in this agent can add or change a tool at runtime. ADK has toolsets that load tools at runtime (`tool/mcptoolset`, `tool/skilltoolset`). The Kit must not give these to an agent, or it must pin them to a reviewed list.
- `adkrest` refuses a request from a browser origin that is not in `AllowedOrigins`, and it has no auth by default (`authn.Noop`). The A2A handler has no origin check. Both need auth before they are used beyond a laptop.
- ADK v2.8.0 pulls in many modules (Google Cloud, OpenTelemetry, GORM). The spike binary is about 81 MB. The Kit's own interfaces keep a later change of framework inside the Kit.
