import { useCallback, useEffect, useRef, useState } from "react";
import type { ActionPayload } from "@a2ui/web_core";
import type { Agent } from "../lib/storage";
import type { Item, Thread } from "../lib/thread";
import { agentColor, clock, initial, toolLabel } from "../lib/look";
import { A2uiBlock } from "./A2uiBlock";
import { Approval } from "./Approval";
import { IconMenu, IconMic } from "./icons";
import { Recorder, transcribe } from "../lib/voice";

export type ThreadActions = {
  send: (text: string, voice?: boolean) => void;
  addToDashboard: (itemId: string) => Promise<void>;
  action: (a: ActionPayload) => void;
  decide: (confirmationId: string, approve: boolean) => void;
};

export function ThreadView({ agent, thread, busy, onMenu, actions }: {
  agent: Agent;
  thread?: Thread;
  busy: boolean;
  onMenu: () => void;
  actions: ThreadActions;
}) {
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => { end.current?.scrollIntoView({ block: "end" }); }, [thread?.items.length, thread?.streaming]);
  const color = agentColor(agent.id);
  const items = thread?.items ?? [];

  return (
    <main className="main">
      <header className="topbar">
        <button type="button" className="icon-btn menu-btn" aria-label="Open the sidebar" onClick={onMenu}><IconMenu /></button>
        <span className="agent-orb">
          {busy && <span className="ring" />}
          <span className="core" style={{ background: color }}>{initial(agent.name)}</span>
        </span>
        <div className="stack" style={{ flex: 1 }}>
          <span className="top-title">{thread?.title ?? "New thread"}</span>
          <span className="meta">{agent.name}{agent.description ? ` · ${agent.description}` : ""}</span>
        </div>
      </header>

      <div className="scroll">
        <div className="column" aria-live="polite">
          {items.length === 0 && !busy && <div className="blank">Ask {agent.name} something to start the thread.</div>}
          {items.length > 0 && <div className="day">{new Date(items[0]!.at).toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "short" })}, {clock(items[0]!.at)}</div>}
          {group(items).map((g, i) =>
            g.who === "user" ? (
              <UserMsg key={i} items={g.items} />
            ) : (
              <AgentMsg key={i} agent={agent} color={color} items={g.items} busy={busy} actions={actions} />
            ),
          )}
          {thread?.streaming && (
            <div className="msg">
              <span className="avatar sm" style={{ background: color }}>{initial(agent.name)}</span>
              <div className="msg-body"><p className="msg-text">{thread.streaming}</p></div>
            </div>
          )}
          {busy && !thread?.streaming && (
            <div className="working">
              <span className="avatar sm" style={{ background: color }}>{initial(agent.name)}</span>
              <span className="bars" aria-hidden="true"><span /><span /><span /></span>
              <span>{agent.name} is working{runningTool(items)}</span>
            </div>
          )}
          <div ref={end} />
        </div>
      </div>

      <Composer agent={agent} busy={busy} onSend={actions.send} />
    </main>
  );
}

function runningTool(items: Item[]): string {
  const t = items.findLast((i) => i.kind === "tool" && i.state === "running");
  return t && t.kind === "tool" ? ` · ${t.name}` : "";
}

type Group = { who: "user" | "agent"; items: Item[] };
function group(items: Item[]): Group[] {
  const out: Group[] = [];
  for (const it of items) {
    const who = it.kind === "user" ? "user" : "agent";
    const last = out.at(-1);
    if (last && last.who === who && who === "agent") last.items.push(it);
    else out.push({ who, items: [it] });
  }
  return out;
}

function UserMsg({ items }: { items: Item[] }) {
  const it = items[0]!;
  if (it.kind !== "user") return null;
  return (
    <div className="msg">
      <span className="avatar sm" style={{ background: "#F2A65A", color: "#1A1206" }}>Y</span>
      <div className="msg-body">
        <div className="msg-head"><b>You</b> <span>· {it.voice ? "voice" : "typed"} · {clock(it.at)}</span></div>
        <p className="msg-text">{it.text}</p>
      </div>
    </div>
  );
}

function AgentMsg({ agent, color, items, busy, actions }: { agent: Agent; color: string; items: Item[]; busy: boolean; actions: ThreadActions }) {
  const tools = items.filter((i) => i.kind === "tool");
  return (
    <div className="msg">
      <span className="avatar sm" style={{ background: color }}>{initial(agent.name)}</span>
      <div className="msg-body">
        <div className="msg-head"><b>{agent.name}</b> <span>· {clock(items[0]!.at)}</span></div>
        {tools.length > 0 && (
          <div className="chips">
            {tools.map((t) => t.kind === "tool" && <span key={t.id} className={`chip ${t.state}`}>{toolLabel(t.name, t.state, t.ms)}</span>)}
          </div>
        )}
        {items.map((it) => {
          switch (it.kind) {
            case "agent":
              return <p key={it.id} className="msg-text">{it.text}</p>;
            case "error":
              return <p key={it.id} className="error-line" role="alert">{it.text}</p>;
            case "surface":
              return it.title ? (
                <ToolCard key={it.id} item={it} onAdd={() => actions.addToDashboard(it.id)} />
              ) : (
                <A2uiBlock key={it.id} messages={it.messages} onAction={busy ? undefined : actions.action} />
              );
            case "approval":
              return <Approval key={it.id} item={it} busy={busy} onAction={actions.action} onDecide={(ok) => actions.decide(it.confirmationId, ok)} />;
            default:
              return null;
          }
        })}
      </div>
    </div>
  );
}

function ToolCard({ item, onAdd }: { item: Extract<Item, { kind: "surface" }>; onAdd: () => Promise<void> }) {
  const [state, setState] = useState<"idle" | "busy" | "error">("idle");
  return (
    <section className="tool-card" aria-label={item.title}>
      <div className="tool-card-head">
        <span className="kit-card-title">{item.title}</span>
        {item.onDashboard ? (
          <span className="ok-line">On the dashboard</span>
        ) : (
          <button
            type="button"
            className="btn small"
            disabled={state === "busy"}
            onClick={() => {
              setState("busy");
              onAdd().then(() => setState("idle"), () => setState("error"));
            }}
          >
            Add to dashboard
          </button>
        )}
      </div>
      {state === "error" && <div className="error-line" role="alert">The agent did not keep the card.</div>}
      <div className="tool-card-body"><A2uiBlock messages={item.messages} /></div>
    </section>
  );
}

function Composer({ agent, busy, onSend }: { agent: Agent; busy: boolean; onSend: (t: string, voice?: boolean) => void }) {
  const [text, setText] = useState("");
  const voice = useHoldToTalk(agent, (said) => onSend(said, true));
  const submit = () => {
    const t = text.trim();
    if (!t || busy) return;
    onSend(t);
    setText("");
  };
  const holdKeys = {
    onKeyDown: (e: React.KeyboardEvent) => {
      if ((e.key === " " || e.key === "Enter") && !e.repeat && e.currentTarget.tagName === "BUTTON") {
        e.preventDefault();
        void voice.start();
      }
      if (e.key === "Escape") void voice.stop(true);
    },
    onKeyUp: (e: React.KeyboardEvent) => {
      if ((e.key === " " || e.key === "Enter") && e.currentTarget.tagName === "BUTTON") void voice.stop();
    },
  };
  return (
    <div className="composer-wrap">
      <div className="composer-col">
        {voice.state === "recording" ? (
          <div className="composer recording" role="status" aria-label="Recording">
            <span className="rec-dot" aria-hidden="true" />
            <span className="mono rec-time">{voice.clock}</span>
            <span className="bars" aria-hidden="true"><span /><span /><span /><span /><span /><span /></span>
            <span className="rec-hint">Listening…</span>
            <button
              type="button"
              className="talk-btn"
              onPointerUp={() => void voice.stop()}
              onPointerLeave={(e) => e.buttons && void voice.stop()}
              onKeyUp={holdKeys.onKeyUp}
              onKeyDown={holdKeys.onKeyDown}
              autoFocus
            >
              Release to send
            </button>
          </div>
        ) : (
          <form className="composer" onSubmit={(e) => { e.preventDefault(); submit(); }}>
            <label htmlFor="msg" className="sr-only">Message {agent.name}</label>
            <input
              id="msg"
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === " " && !text && !e.repeat) {
                  e.preventDefault();
                  void voice.start();
                }
              }}
              onKeyUp={(e) => e.key === " " && !text && void voice.stop()}
              placeholder={voice.state === "working" ? "Turning your words into text…" : `Type to ${agent.name}, or hold Space to talk`}
              autoComplete="off"
              disabled={voice.state === "working"}
            />
            {text.trim() ? (
              <button type="submit" className="send-btn" disabled={busy}>Send</button>
            ) : (
              <button
                type="button"
                className="talk-btn"
                aria-label={`Hold to talk to ${agent.name}`}
                disabled={voice.state === "working"}
                onPointerDown={(e) => { e.preventDefault(); void voice.start(); }}
                onPointerUp={() => void voice.stop()}
                onKeyDown={holdKeys.onKeyDown}
                onKeyUp={holdKeys.onKeyUp}
              >
                <IconMic /> Hold to talk
              </button>
            )}
          </form>
        )}
        {voice.error && <div className="error-line" role="alert">{voice.error}</div>}
        <div className="hints">
          <span><b>Space</b> hold to talk</span>
          <span><b>Enter</b> send text</span>
          <span><b>Esc</b> cancel</span>
          <span style={{ flex: 1 }} />
          <span>Keys stay on the agent. This page holds none.</span>
        </div>
      </div>
    </div>
  );
}

// useHoldToTalk records while a person holds, then sends the audio to the
// agent for text.
function useHoldToTalk(agent: Agent, onText: (t: string) => void) {
  const rec = useRef<{ r: Recorder; ready: Promise<boolean> } | undefined>(undefined);
  const started = useRef(0);
  const [state, setState] = useState<"idle" | "recording" | "working">("idle");
  const [error, setError] = useState<string>();
  const [now, setNow] = useState(0);

  useEffect(() => {
    if (state !== "recording") return;
    const t = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(t);
  }, [state]);

  const start = useCallback(async () => {
    if (rec.current) return;
    setError(undefined);
    const r = new Recorder();
    started.current = Date.now();
    const ready = r.start().then(
      () => true,
      () => false,
    );
    rec.current = { r, ready };
    if (await ready) {
      if (rec.current?.r === r) {
        setNow(Date.now());
        setState("recording");
      }
    } else {
      if (rec.current?.r === r) rec.current = undefined;
      setError("The browser did not give the microphone. Allow it for this page, then try again.");
    }
  }, []);

  const stop = useCallback(
    async (cancel = false) => {
      const cur = rec.current;
      if (!cur) return;
      rec.current = undefined;
      const short = Date.now() - started.current < 400;
      if (!(await cur.ready)) return;
      const blob = await cur.r.stop(cancel || short);
      if (!blob) {
        setState("idle");
        if (short && !cancel) setError("Hold the button while you talk.");
        return;
      }
      setState("working");
      try {
        const said = await transcribe(agent, blob);
        if (said) onText(said);
        else setError("No speech was heard.");
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setState("idle");
      }
    },
    [agent, onText],
  );

  const secs = Math.max(0, Math.floor((now - started.current) / 1000));
  return { state, error, start, stop, clock: `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}` };
}
