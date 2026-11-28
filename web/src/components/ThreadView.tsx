import { useEffect, useRef, useState } from "react";
import type { ActionPayload } from "@a2ui/web_core";
import type { Agent } from "../lib/storage";
import type { Item, Thread } from "../lib/thread";
import { agentColor, clock, initial, toolLabel } from "../lib/look";
import { A2uiBlock } from "./A2uiBlock";
import { Approval } from "./Approval";
import { IconMenu } from "./icons";

export type ThreadActions = {
  send: (text: string) => void;
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
              return <A2uiBlock key={it.id} messages={it.messages} onAction={busy ? undefined : actions.action} />;
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

function Composer({ agent, busy, onSend }: { agent: Agent; busy: boolean; onSend: (t: string) => void }) {
  const [text, setText] = useState("");
  const submit = () => {
    const t = text.trim();
    if (!t || busy) return;
    onSend(t);
    setText("");
  };
  return (
    <div className="composer-wrap">
      <div className="composer-col">
        <form className="composer" onSubmit={(e) => { e.preventDefault(); submit(); }}>
          <label htmlFor="msg" className="sr-only">Message {agent.name}</label>
          <input id="msg" value={text} onChange={(e) => setText(e.target.value)} placeholder={`Type to ${agent.name}`} autoComplete="off" />
          <button type="submit" className="send-btn" disabled={busy || !text.trim()}>Send</button>
        </form>
        <div className="hints">
          <span><b>Enter</b> send text</span>
          <span style={{ flex: 1 }} />
          <span>Keys stay on the agent. This page holds none.</span>
        </div>
      </div>
    </div>
  );
}
