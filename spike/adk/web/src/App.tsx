import { useEffect, useRef, useState } from "react";
import { A2uiSurface } from "@a2ui/react";
import { MessageProcessor, type ActionPayload } from "@a2ui/web_core";
import { basicCatalog } from "@a2ui/web_core/catalogs/basic/v1";
import { A2UI_MIME, send, type Part } from "./a2a";

type Line = { who: "you" | "agent" | "info"; text: string };

export function App() {
  const contextId = useRef(crypto.randomUUID());
  const taskId = useRef<string | undefined>(undefined);
  const [lines, setLines] = useState<Line[]>([]);
  const [input, setInput] = useState("Write a note called Groceries with the text: milk, eggs, bread.");
  const [busy, setBusy] = useState(false);

  const handle = useRef<(a: ActionPayload) => void>(() => {});
  const [processor] = useState(() => new MessageProcessor([basicCatalog], (a) => handle.current(a)));
  const [surfaces, setSurfaces] = useState(() => [...processor.model.surfacesMap.values()]);
  useEffect(() => {
    const sync = () => setSurfaces([...processor.model.surfacesMap.values()]);
    const a = processor.onSurfaceCreated(sync);
    const b = processor.onSurfaceDeleted(sync);
    return () => { a.unsubscribe(); b.unsubscribe(); };
  }, [processor]);

  async function exchange(parts: Part[], shown: Line) {
    setBusy(true);
    setLines((l) => [...l, shown]);
    try {
      const reply = await send(contextId.current, parts, taskId.current);
      taskId.current = reply.state === "TASK_STATE_INPUT_REQUIRED" ? reply.taskId : undefined;
      for (const p of reply.parts) {
        if (p.metadata?.mimeType === A2UI_MIME) processor.processMessages(p.data as never);
        else if (p.text) setLines((l) => [...l, { who: "agent", text: p.text! }]);
      }
      setLines((l) => [...l, { who: "info", text: `task state: ${reply.state ?? "message"}` }]);
    } catch (e) {
      setLines((l) => [...l, { who: "info", text: String(e) }]);
    } finally {
      setBusy(false);
    }
  }

  handle.current = (a) => {
    processor.processMessages([{ version: "1.0", deleteSurface: { surfaceId: a.surfaceId } }] as never);
    const action = { name: a.name, surfaceId: a.surfaceId, sourceComponentId: a.sourceComponentId, timestamp: a.timestamp, context: a.context };
    void exchange([{ data: [{ version: "1.0", action }], metadata: { mimeType: A2UI_MIME } }], { who: "you", text: `[${a.name}]` });
  };

  return (
    <main style={{ fontFamily: "system-ui", maxWidth: 640, margin: "2rem auto" }}>
      <h1>ADK spike: A2UI over A2A</h1>
      {lines.map((l, i) => (
        <p key={i} style={{ color: l.who === "info" ? "#888" : undefined }}>
          <b>{l.who}:</b> {l.text}
        </p>
      ))}
      {surfaces.map((s) => (
        <div key={s.id} data-surface={s.id} style={{ border: "1px solid #ccc", padding: 12, margin: "12px 0" }}>
          <A2uiSurface surface={s} />
        </div>
      ))}
      <form onSubmit={(e) => { e.preventDefault(); if (input.trim()) void exchange([{ text: input }], { who: "you", text: input }); setInput(""); }}>
        <input value={input} onChange={(e) => setInput(e.target.value)} style={{ width: "80%" }} disabled={busy} />
        <button disabled={busy}>Send</button>
      </form>
    </main>
  );
}
