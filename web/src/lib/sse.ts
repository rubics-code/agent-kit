// Reads a text/event-stream body and gives the JSON of each event's data.
export async function* readSSE(body: ReadableStream<Uint8Array>): AsyncGenerator<unknown> {
  const reader = body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { value, done } = await reader.read();
    buf += value ? dec.decode(value, { stream: true }) : dec.decode();
    let cut: number;
    while ((cut = buf.search(/\r?\n\r?\n/)) >= 0) {
      const block = buf.slice(0, cut);
      buf = buf.slice(cut).replace(/^\r?\n\r?\n/, "");
      const data = parseBlock(block);
      if (data !== undefined) yield data;
    }
    if (done) break;
  }
  const last = parseBlock(buf);
  if (last !== undefined) yield last;
}

export function parseBlock(block: string): unknown {
  const lines = block
    .split(/\r?\n/)
    .filter((l) => l.startsWith("data:"))
    .map((l) => l.slice(5).replace(/^ /, ""));
  if (lines.length === 0) return undefined;
  return JSON.parse(lines.join("\n"));
}
