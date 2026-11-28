import { useState } from "react";
import type { DashCard } from "../lib/dashboard";
import { lastChange } from "../lib/dashboard";
import { agentColor, ago, initial } from "../lib/look";
import type { Agent } from "../lib/storage";
import type { Thread } from "../lib/thread";
import { A2uiBlock } from "./A2uiBlock";
import { IconMenu } from "./icons";

export function Dashboard({ agents, cards, threads, busy, error, now, onAsk, onRemove, onMenu }: {
  agents: Agent[];
  cards: DashCard[];
  threads: Thread[];
  busy: Record<string, boolean>;
  error?: string;
  now: number;
  onAsk: (text: string, to: Agent) => string | undefined;
  onRemove: (c: DashCard) => void;
  onMenu: () => void;
}) {
  const [to, setTo] = useState(agents[0]?.id ?? "");
  const [text, setText] = useState("");
  const [askId, setAskId] = useState<string>();
  const change = lastChange(cards);
  const target = agents.find((a) => a.id === to) ?? agents[0];
  const ask = threads.find((t) => t.id === askId);
  const reply = ask?.items.findLast((i) => i.kind === "agent" || i.kind === "error");

  return (
    <main className="main">
      <header className="dash-head">
        <div className="dash-title-row">
          <button type="button" className="icon-btn menu-btn" aria-label="Open the sidebar" onClick={onMenu}><IconMenu /></button>
          <h1>Dashboard</h1>
          <span className="meta">{change ? `Last change ${ago(change.at, now)}: ${change.text}` : "No cards yet"}</span>
        </div>
        <form
          className="ask-bar"
          onSubmit={(e) => {
            e.preventDefault();
            const t = text.trim();
            if (!t || !target) return;
            setAskId(onAsk(t, target));
            setText("");
          }}
        >
          <label htmlFor="ask-agent" className="sr-only">The agent</label>
          <select id="ask-agent" value={target?.id ?? ""} onChange={(e) => setTo(e.target.value)} disabled={agents.length === 0}>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
          <label htmlFor="ask" className="sr-only">Ask for a card</label>
          <input id="ask" value={text} onChange={(e) => setText(e.target.value)} placeholder="Ask for a card, for example “keep my open tasks here”" autoComplete="off" />
          <button type="submit" className="send-btn" disabled={!text.trim() || !target}>Ask</button>
        </form>
        {ask && (
          <div className="meta ask-status" aria-live="polite">
            {busy[ask.id] ? `${target?.name ?? "The agent"} is working…` : reply ? `${reply.kind === "error" ? "Error" : agents.find((a) => a.id === ask.agentId)?.name}: ${reply.text}` : ""}
            {" "}<a href={`#/t/${ask.id}`}>Open the thread</a>
          </div>
        )}
        {error && <div className="error-line" role="alert">{error}</div>}
      </header>

      <div className="dash-grid">
        {cards.map((c) => (
          <section key={`${c.agentId}/${c.cardId}`} className="dash-card" aria-label={c.title}>
            <div className="dash-card-head">
              <span className="avatar xs" style={{ background: agentColor(c.agentId) }} title={c.agentName}>{initial(c.agentName)}</span>
              <span className="kit-card-title">{c.title}</span>
              <span className="meta">Updated {ago(Date.parse(c.updatedAt), now)}</span>
              <button type="button" className="link-btn" aria-label={`Remove ${c.title}`} onClick={() => onRemove(c)}>Remove</button>
            </div>
            <div className="dash-card-body">
              <A2uiBlock messages={c.a2ui} />
            </div>
            {c.threadId && threads.some((t) => t.id === c.threadId) && (
              <a className="dash-card-foot" href={`#/t/${c.threadId}`}>From the thread “{threads.find((t) => t.id === c.threadId)?.title}” →</a>
            )}
          </section>
        ))}
        <section className="dash-empty">
          <span className="title-sm">Add a card</span>
          <span>Ask an agent in the box above, or click “Add to dashboard” on a card in any thread.</span>
        </section>
      </div>
    </main>
  );
}
