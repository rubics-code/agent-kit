// A minimal A2A v1.0 JSON-RPC client. It sends the A2UI renderer capabilities
// on each message, as the A2UI A2A extension needs.

export const A2UI_MIME = "application/a2ui+json";
export const A2UI_EXTENSION = "https://a2ui.org/a2a-extension/a2ui/v1.0";
export const BASIC_CATALOG = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json";

export type Part = { text?: string; data?: unknown; metadata?: Record<string, unknown> };
export type Reply = { taskId?: string; state?: string; parts: Part[] };

export async function send(contextId: string, outParts: Part[], taskId?: string): Promise<Reply> {
  const res = await fetch("/a2a", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-A2A-Extensions": A2UI_EXTENSION, "A2A-Version": "1.0" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: crypto.randomUUID(),
      method: "SendMessage",
      params: {
        message: {
          messageId: crypto.randomUUID(),
          contextId,
          taskId,
          role: "ROLE_USER",
          parts: outParts,
          metadata: { a2uiRendererCapabilities: { "1.0": { supportedCatalogIds: [BASIC_CATALOG] } } },
        },
      },
    }),
  });
  const body = await res.json();
  if (body.error) throw new Error(JSON.stringify(body.error));
  const task = body.result?.task;
  if (!task) return { parts: body.result?.message?.parts ?? [] };
  const parts: Part[] = [];
  for (const a of task.artifacts ?? []) parts.push(...(a.parts ?? []));
  parts.push(...(task.status?.message?.parts ?? []));
  return { taskId: task.id, state: task.status?.state, parts };
}
