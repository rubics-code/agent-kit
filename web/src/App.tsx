import { useEffect, useState } from "react";
import { Connect } from "./components/Connect";
import { Dashboard } from "./components/Dashboard";
import { ago } from "./lib/look";
import { lastChange } from "./lib/dashboard";
import { Sidebar } from "./components/Sidebar";
import { ThreadView } from "./components/ThreadView";
import { useConsole } from "./lib/useConsole";

export function App() {
  const c = useConsole();
  const [navOpen, setNavOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => { setNavOpen(false); }, [c.route.name, c.threadId]);

  return (
    <div className={`app${navOpen ? " nav-open" : ""}`}>
      <Sidebar
        agents={c.agents}
        threads={c.threads}
        agentId={c.agent?.id}
        threadId={c.threadId}
        route={c.route.name}
        busyThread={c.busyThread}
        dashboardMeta={dashboardMeta(c.cards, now)}
        now={now}
        onAgent={c.selectAgent}
        onThread={(id) => (location.hash = `#/t/${id}`)}
        onNewThread={c.newThread}
      />
      <button type="button" className="scrim" aria-label="Close the sidebar" onClick={() => setNavOpen(false)} />
      {c.route.name === "thread" && c.agent && (
        <ThreadView
          agent={c.agent}
          thread={c.thread}
          busy={c.busy}
          onMenu={() => setNavOpen(true)}
          actions={{ send: (t) => void c.send(t), action: c.action, decide: c.decideDirect, addToDashboard: c.addToDashboard }}
        />
      )}
      {c.route.name === "thread" && !c.agent && (
        <main className="main"><div className="blank">No agent is connected. Use “Connect an agent” in the sidebar.</div></main>
      )}
      {c.route.name === "connect" && (
        <Connect agents={c.agents} onConnect={c.connect} onRemove={c.removeAgent} onClose={() => history.back()} />
      )}
      {c.route.name === "dashboard" && (
        <Dashboard
          agents={c.agents}
          cards={c.cards}
          threads={c.threads}
          busy={c.busyMap}
          error={c.dashError}
          now={now}
          onAsk={(text, to) => c.send(text, { to })}
          onRemove={(card) => void c.deleteCard(card)}
          onMenu={() => setNavOpen(true)}
        />
      )}
    </div>
  );
}

function dashboardMeta(cards: { updatedAt: string }[], now: number): string {
  if (cards.length === 0) return "No cards yet";
  const change = lastChange(cards as never);
  return `${cards.length} card${cards.length === 1 ? "" : "s"} · updated ${change ? ago(change.at, now) : ""}`;
}
