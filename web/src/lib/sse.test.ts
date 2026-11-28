import { describe, expect, it } from "vitest";
import { parseBlock, readSSE } from "./sse";

function body(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch));
      c.close();
    },
  });
}

describe("readSSE", () => {
  it("joins events that arrive in pieces", async () => {
    const out: unknown[] = [];
    for await (const e of readSSE(body(['id: 1\ndata: {"a"', ":1}\n\nid: 2\r\n", 'data: {"b":2}\r\n\r\n', 'data: {"c":3}']))) out.push(e);
    expect(out).toEqual([{ a: 1 }, { b: 2 }, { c: 3 }]);
  });
  it("skips a block with no data", () => {
    expect(parseBlock(": comment\nid: 3")).toBeUndefined();
  });
});
