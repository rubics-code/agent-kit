import type { Agent } from "../lib/storage";
import type { Thread } from "../lib/thread";
import { agentColor, ago, initial } from "../lib/look";
import { IconBoard, IconLogo, IconPlus } from "./icons";

export function Sidebar({ agents, threads, agentId, threadId, route, busyThread, dashboardMeta, now, onAgent, onThread, onNewThread }: {
  agents: Agent[];
  threads: Thread[];
  agentId?: string;
  threadId?: string;
  route: string;
  busyThread?: string;
  dashboardMeta: string;
  now: number;
  onAgent: (id: string) => void;
  onThread: (id: string) => void;
  onNewThread: () => void;
}) {
  const agent = agents.find((a) => a.id === agentId);
  const mine = threads.filter((t) => t.agentId === agentId).sort((a, b) => b.updatedAt - a.updatedAt);
  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true"><IconLogo /></span>
        Agent Console
      </div>

      <a href="#/dashboard" className="dash-link" aria-current={route === "dashboard" ? "page" : undefined}>
        <span className="icon-tile"><IconBoard /></span>
        <span className="stack">
          <span className="title-sm">Dashboard</span>
          <span className="meta">{dashboardMeta}</span>
        </span>
      </a>

      <nav aria-label="Agents" className="stack" style={{ gap: 2 }}>
        <div className="side-label">Agents</div>
        {agents.length === 0 && <div className="empty-note">No agent yet. Connect one below.</div>}
        {agents.map((a) => (
          <button key={a.id} type="button" className="row-btn" aria-current={a.id === agentId && route === "thread"} onClick={() => onAgent(a.id)}>
            <span className="avatar" style={{ background: agentColor(a.id) }}>
              {initial(a.name)}
              <span className="dot" style={{ background: threads.some((t) => t.id === busyThread && t.agentId === a.id) ? "#F0616D" : "#4CC38A" }} />
            </span>
            <span className="stack">
              <span className="title-sm">{a.name}</span>
              <span className="meta">{threads.some((t) => t.id === busyThread && t.agentId === a.id) ? "Working" : a.description || "Idle"}</span>
            </span>
          </button>
        ))}
      </nav>

      {agent && (
        <nav aria-label={`Threads with ${agent.name}`} className="stack" style={{ gap: 2, minHeight: 0 }}>
          <div className="side-label">
            <span>Threads · {agent.name}</span>
            <button type="button" className="icon-btn" aria-label="New thread" onClick={onNewThread}><IconPlus /></button>
          </div>
          {mine.length === 0 && <div className="empty-note">No threads yet.</div>}
          {mine.map((t) => (
            <button key={t.id} type="button" className="row-btn thread-row" aria-current={t.id === threadId && route === "thread"} onClick={() => onThread(t.id)}>
              <span className="stack" style={{ flex: 1, gap: 2 }}>
                <span className="meta" style={{ color: "var(--text)", fontSize: 13 }}>{t.title}</span>
                <span className="meta" style={{ color: "var(--faint)" }}>{ago(t.updatedAt, now)}{t.pendingTaskId ? " · waits for you" : ""}</span>
              </span>
              {t.id === busyThread && <span className="live-tag">LIVE</span>}
            </button>
          ))}
        </nav>
      )}

      <div className="side-foot">
        <a href="#/connect" className="connect-link"><IconPlus /> Connect an agent</a>
      </div>
    </aside>
  );
}
