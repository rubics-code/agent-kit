import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { type IncomingMessage, type ServerResponse, createServer } from "node:http";
import type { AddressInfo } from "node:net";
import type { AgentGraph } from "../agent.ts";
import type { ApprovalDecision } from "../../approval.ts";
import type { GroupStatus } from "../../group.ts";
import type { Transcriber } from "../transcribe.ts";
import { type Step, resume, say } from "../turns.ts";

// What /api/thread tells the page. The Tools panel renders `groups`.
export interface AgentInfo {
  name?: string;
  model: string;
  groups: GroupStatus[];
  keys: string;
}

const PAGE = readFileSync(new URL("./index.html", import.meta.url), "utf8");

// The name goes into HTML and into script strings, so it may hold only safe characters.
const SAFE_NAME = /^[A-Za-z0-9 _-]{1,40}$/;

export function page(name: string): string {
  if (!SAFE_NAME.test(name)) throw new Error(`agent name "${name}" must be 1 to 40 letters, digits, spaces, _ or -`);
  return PAGE.replaceAll("{{NAME}}", name);
}
const MAX_BODY = 64 * 1024;
// 16 kHz mono 16-bit WAV is 32 kB a second: this is about two minutes.
const MAX_AUDIO = 4 * 1024 * 1024;

// Local web chat. It listens on 127.0.0.1 only. The Host check stops DNS rebinding.
// An API call must be JSON, and its Origin, if it has one, must be this server.
// A page on another site can send JSON only after a CORS preflight, and this
// server never allows one, so such a page cannot approve the agent's writes.
export async function startWeb(
  agent: AgentGraph,
  opts: { port?: number; info?: AgentInfo; transcribe?: Transcriber } = {},
) {
  const pending = new Map<string, Map<string, string>>(); // thread -> interrupt id -> args hash
  const busy = new Set<string>();
  let port = 0;
  const name = opts.info?.name ?? "Agent";
  const html = page(name);

  const okHost = (h: string | undefined) => h === `127.0.0.1:${port}` || h === `localhost:${port}`;
  const okOrigin = (o: string | undefined) =>
    o === undefined || o === `http://localhost:${port}` || o === `http://127.0.0.1:${port}`;

  function send(res: ServerResponse, status: number, body: unknown) {
    res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
    res.end(JSON.stringify(body));
  }

  async function body(req: IncomingMessage, max = MAX_BODY): Promise<Record<string, unknown>> {
    let size = 0;
    const chunks: Buffer[] = [];
    for await (const c of req) {
      size += (c as Buffer).length;
      if (size > max) throw new Error("request too large");
      chunks.push(c as Buffer);
    }
    const value: unknown = JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("expected a JSON object");
    return value as Record<string, unknown>;
  }

  function remember(thread: string, step: Step) {
    if (step.kind === "approvals") pending.set(thread, new Map(step.items.map((i) => [i.id, i.argsHash])));
    else pending.delete(thread);
    return step.kind === "approvals"
      ? { approvals: step.items.map(({ id, tool, summary, argsHash }) => ({ id, tool, summary, argsHash })) }
      : { reply: step.text };
  }

  async function api(req: IncomingMessage, res: ServerResponse, path: string) {
    if (req.method !== "POST") return send(res, 405, { error: "use POST" });
    if (!okOrigin(req.headers.origin)) return send(res, 403, { error: "bad origin" });
    if (!String(req.headers["content-type"] ?? "").startsWith("application/json")) {
      return send(res, 415, { error: "use application/json" });
    }

    if (path === "/api/transcribe") {
      if (!opts.transcribe) return send(res, 501, { error: "speech to text is not set up" });
      const b = await body(req, Math.ceil(MAX_AUDIO * 1.4));
      const wav = typeof b.audio === "string" ? Buffer.from(b.audio, "base64") : Buffer.alloc(0);
      if (wav.length < 44 || wav.toString("ascii", 0, 4) !== "RIFF" || wav.toString("ascii", 8, 12) !== "WAVE") {
        return send(res, 400, { error: "audio must be a WAV file" });
      }
      if (wav.length > MAX_AUDIO) return send(res, 413, { error: "recording is too long; keep it under two minutes" });
      return send(res, 200, { text: await opts.transcribe(wav) });
    }

    const b = await body(req);
    if (path === "/api/thread") return send(res, 200, { thread: randomUUID(), ...opts.info, speech: !!opts.transcribe });

    const thread = typeof b.thread === "string" && /^[0-9a-f-]{36}$/.test(b.thread) ? b.thread : undefined;
    if (!thread) return send(res, 400, { error: "thread is missing" });
    if (busy.has(thread)) return send(res, 409, { error: `${name} is still busy with this conversation` });

    busy.add(thread);
    try {
      if (path === "/api/say") {
        if (pending.has(thread)) return send(res, 409, { error: "decide the open approvals first" });
        const text = typeof b.text === "string" ? b.text.trim() : "";
        if (!text || text.length > 8000) return send(res, 400, { error: "text must be 1 to 8000 characters" });
        return send(res, 200, remember(thread, await say(agent, thread, text)));
      }
      if (path === "/api/decide") {
        const open = pending.get(thread);
        const given = (b.decisions ?? {}) as Record<string, { approved?: unknown; reason?: unknown }>;
        if (!open) return send(res, 409, { error: "nothing waits for approval" });
        if (Object.keys(given).sort().join() !== [...open.keys()].sort().join()) {
          return send(res, 400, { error: "decide each open approval" });
        }
        const decisions: Record<string, ApprovalDecision> = {};
        for (const [id, hash] of open) {
          const d = given[id]!;
          decisions[id] = {
            approved: d.approved === true,
            argsHash: hash,
            ...(typeof d.reason === "string" && d.reason ? { reason: d.reason.slice(0, 500) } : {}),
          };
        }
        return send(res, 200, remember(thread, await resume(agent, thread, decisions)));
      }
      return send(res, 404, { error: "not found" });
    } finally {
      busy.delete(thread);
    }
  }

  const server = createServer((req, res) => {
    if (!okHost(req.headers.host)) {
      res.writeHead(421).end();
      return;
    }
    const path = new URL(req.url ?? "/", "http://x").pathname;
    if (path === "/" && req.method === "GET") {
      res.writeHead(200, {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
        "Content-Security-Policy":
          "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
        "Referrer-Policy": "no-referrer",
        "X-Content-Type-Options": "nosniff",
      });
      res.end(html);
      return;
    }
    if (path.startsWith("/api/")) {
      api(req, res, path).catch((err: unknown) => {
        if (!res.headersSent) send(res, 500, { error: err instanceof Error ? err.message : String(err) });
      });
      return;
    }
    res.writeHead(404).end();
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(opts.port ?? 4141, "127.0.0.1", () => resolve());
  });
  port = (server.address() as AddressInfo).port;
  return { server, port, url: `http://localhost:${port}/` };
}
