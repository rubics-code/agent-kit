// A small A2A 1.0 JSON-RPC client. The agent holds every key; this client
// sends text and A2UI actions, and reads the events.
import { readSSE } from "./sse";

export const A2UI_MIME = "application/a2ui+json";
export const A2UI_EXTENSION = "https://a2ui.org/a2a-extension/a2ui/v1.0";
export const BASIC_CATALOG = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json";

export type Part = { text?: string; data?: unknown; metadata?: Record<string, unknown> };
export type Message = { messageId: string; role: string; parts: Part[]; contextId?: string; taskId?: string; metadata?: Record<string, unknown> };
export type TaskStatus = { state: string; message?: Message };
export type Task = { id: string; contextId: string; status: TaskStatus; artifacts?: Artifact[] };
export type Artifact = { artifactId: string; parts: Part[]; metadata?: Record<string, unknown> };

export type StreamEvent =
  | { task: Task }
  | { message: Message }
  | { statusUpdate: { taskId: string; contextId: string; status: TaskStatus } }
  | { artifactUpdate: { taskId: string; contextId: string; artifact: Artifact; append?: boolean; lastChunk?: boolean } }
  | { error: string };

export type AgentCard = {
  name: string;
  description?: string;
  version?: string;
  supportedInterfaces?: { url: string; protocolBinding?: string; protocolVersion?: string }[];
  capabilities?: { streaming?: boolean; extensions?: { uri: string; params?: Record<string, unknown> }[] };
  skills?: { id?: string; name: string; description?: string }[];
};

export class A2AError extends Error {}

export function newId(): string {
  return crypto.randomUUID();
}

export function userMessage(contextId: string, parts: Part[], catalogs: string[], taskId?: string): Message {
  return {
    messageId: newId(),
    role: "ROLE_USER",
    contextId,
    ...(taskId ? { taskId } : {}),
    parts,
    metadata: { a2uiRendererCapabilities: { "1.0": { supportedCatalogIds: catalogs } } },
  };
}

export class A2AClient {
  constructor(
    readonly endpoint: string,
    private readonly fetchFn: typeof fetch = (...a) => fetch(...a),
  ) {}

  private request(method: string, params: unknown, accept: string): Promise<Response> {
    return this.fetchFn(this.endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: accept, "A2A-Version": "1.0", "X-A2A-Extensions": A2UI_EXTENSION },
      body: JSON.stringify({ jsonrpc: "2.0", id: newId(), method, params }),
    });
  }

  // stream sends a message and gives each event as it comes.
  async *stream(message: Message, signal?: AbortSignal): AsyncGenerator<StreamEvent> {
    const res = await this.request("SendStreamingMessage", { message }, "text/event-stream");
    if (!res.ok || !res.body) throw new A2AError(`the agent answered ${res.status}`);
    for await (const raw of readSSE(res.body)) {
      if (signal?.aborted) return;
      const o = raw as { result?: StreamEvent; error?: { message?: string } };
      if (o.error) yield { error: o.error.message ?? "unknown error" };
      else if (o.result) yield o.result;
    }
  }

  async send(message: Message): Promise<StreamEvent> {
    const res = await this.request("SendMessage", { message }, "application/json");
    const o = (await res.json()) as { result?: StreamEvent; error?: { message?: string } };
    if (o.error) return { error: o.error.message ?? "unknown error" };
    if (!o.result) throw new A2AError("the agent sent no result");
    return o.result;
  }
}

// fetchCard reads the A2A agent card of the agent at base.
export async function fetchCard(base: string, fetchFn: typeof fetch = (...a) => fetch(...a)): Promise<AgentCard> {
  const url = new URL("/.well-known/agent-card.json", base);
  const res = await fetchFn(url);
  if (!res.ok) throw new A2AError(`no agent card at ${url} (${res.status})`);
  const card = (await res.json()) as AgentCard;
  if (!card.name) throw new A2AError("the agent card has no name");
  return card;
}

// endpointFor gives the JSON-RPC URL in the card. An agent on this page's own
// origin is called through a relative path, so a dev proxy works.
export function endpointFor(card: AgentCard, base: string, pageOrigin: string): string {
  const jsonrpc = card.supportedInterfaces?.find((i) => !i.protocolBinding || i.protocolBinding === "JSONRPC");
  const url = new URL(jsonrpc?.url ?? "/a2a", base);
  if (new URL(base).origin === pageOrigin) return url.pathname;
  return url.toString();
}

export function rendersA2UI(card: AgentCard): boolean {
  return !!card.capabilities?.extensions?.some((e) => e.uri === A2UI_EXTENSION);
}
