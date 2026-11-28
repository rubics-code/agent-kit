import { useEffect, useState } from "react";
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
        dashboardMeta="Comes soon"
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
          actions={{ send: (t) => c.send(t), action: c.action, decide: c.decideDirect }}
        />
      )}
      {c.route.name === "thread" && !c.agent && (
        <main className="main"><div className="blank">No agent is connected. Use “Connect an agent” in the sidebar.</div></main>
      )}
      {c.route.name !== "thread" && (
        <main className="main"><div className="blank">This screen comes in a later step.</div></main>
      )}
    </div>
  );
}
