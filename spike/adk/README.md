# ADK Go spike (phase 0)

A test agent for the Go Kit on Google ADK Go v2. The results are in [FINDINGS.md](FINDINGS.md). This is a spike. It is not part of the npm package.

## Run the agent

Export an OpenRouter key first. Do not put the key in a file in this repo.

```bash
go run . -db /tmp/spike.db -usage-log /tmp/usage.jsonl
```

- REST: `http://127.0.0.1:8080/api` (ADK's `adkrest` routes, for example `POST /api/run`)
- A2A: `http://127.0.0.1:8080/a2a` (JSON-RPC, header `A2A-Version: 1.0`)
- Agent card: `http://127.0.0.1:8080/.well-known/agent-card.json`

Flags: `-toolsearch` hides the filler tools behind `search_tools`. `-fillers N` sets the number of filler tools. `-raw-usage=false` uses ADK's default HTTP client.

## Run the web page

```bash
cd web && npm install && npm run dev
```

Open `http://127.0.0.1:5173`, ask for a note, then approve or reject it on the A2UI card.

## Other commands

- `go test ./...` runs the unit tests. They need no key.
- `go run ./cmd/proxy` starts a CONNECT proxy on port 8899 for the `HTTPS_PROXY` test.
- `scripts/tokens.sh OUTDIR` runs the token test with tool search off and on.
