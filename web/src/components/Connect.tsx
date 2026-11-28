import { useState } from "react";
import type { AgentCard } from "../lib/a2a";
import { KIT_CATALOG } from "../a2ui/kit";
import { check, normalizeAddress, testCall, TEST_PROMPT, toAgent } from "../lib/connect";
import { agentColor, initial } from "../lib/look";
import type { Agent } from "../lib/storage";

type Phase = { name: "idle" } | { name: "busy"; what: string } | { name: "ready"; card: AgentCard; agent: Agent; test?: { ms: number; text: string }; testError?: string } | { name: "error"; text: string };

export function Connect({ agents, onConnect, onRemove, onClose }: {
  agents: Agent[];
  onConnect: (a: Agent) => void;
  onRemove: (id: string) => void;
  onClose: () => void;
}) {
  const [address, setAddress] = useState("");
  const [phase, setPhase] = useState<Phase>({ name: "idle" });

  async function run() {
    let base: string;
    try {
      base = normalizeAddress(address);
    } catch (e) {
      setPhase({ name: "error", text: (e as Error).message });
      return;
    }
    setPhase({ name: "busy", what: "Reading the agent card…" });
    let card: AgentCard;
    try {
      card = await check(base);
    } catch (e) {
      setPhase({ name: "error", text: `${(e as Error).message}. Check the address, and that the agent lets this page's origin call it.` });
      return;
    }
    const agent = toAgent(card, base, location.origin, Date.now());
    setPhase({ name: "ready", card, agent });
    try {
      const test = await testCall(agent);
      setPhase({ name: "ready", card, agent, test });
    } catch (e) {
      setPhase({ name: "ready", card, agent, testError: (e as Error).message });
    }
  }

  const ready = phase.name === "ready" ? phase : undefined;
  const catalogsOf = (c: AgentCard) =>
    (c.capabilities?.extensions ?? []).flatMap((e) => (e.params?.supportedCatalogIds as string[] | undefined) ?? []);

  return (
    <main className="main connect-page">
      <div role="dialog" aria-labelledby="connect-title" className="connect">
        <div className="connect-head">
          <div className="stack" style={{ gap: 6, flex: 1 }}>
            <h1 id="connect-title">Connect an agent</h1>
            <p>Enter the agent's address. The console reads its A2A agent card, then makes a test call. The agent keeps its own keys, so this page asks for none.</p>
          </div>
          <button type="button" className="close-btn" aria-label="Close" onClick={onClose}>✕</button>
        </div>

        <form className="connect-box" onSubmit={(e) => { e.preventDefault(); void run(); }}>
          <label htmlFor="addr" className="title-sm">Address</label>
          <div className="addr-row">
            <input id="addr" className="mono" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="http://127.0.0.1:8080" autoComplete="url" spellCheck={false} />
            <button type="submit" className="btn" disabled={phase.name === "busy"}>Check</button>
          </div>
          {phase.name === "busy" && <span className="meta">{phase.what}</span>}
          {phase.name === "error" && <span className="error-line" role="alert">{phase.text}</span>}
          {ready && (
            <div className="card-found" aria-label="Agent card">
              <span className="avatar" style={{ background: agentColor(ready.agent.id) }}>{initial(ready.card.name)}</span>
              <div className="stack" style={{ gap: 4, flex: 1 }}>
                <span className="title-sm">{ready.card.name}{ready.card.version ? <span className="meta"> · v{ready.card.version}</span> : null}</span>
                {ready.card.description && <span className="meta" style={{ whiteSpace: "normal" }}>{ready.card.description}</span>}
                <span className="ok-line">
                  Found an A2A agent{ready.card.capabilities?.streaming ? ", with streaming" : ""}.
                  {ready.agent.a2ui ? (catalogsOf(ready.card).includes(KIT_CATALOG) ? " It sends Kit cards." : " It sends A2UI cards.") : " It sends text only."}
                </span>
                {ready.card.skills && ready.card.skills.length > 0 && (
                  <span className="meta">Skills: {ready.card.skills.map((s) => s.name).join(", ")}</span>
                )}
              </div>
            </div>
          )}
        </form>

        {agents.length > 0 && (
          <section className="connected">
            <div className="side-label">Connected</div>
            {agents.map((a) => (
              <div key={a.id} className="connected-row">
                <span className="avatar xs" style={{ background: agentColor(a.id) }}>{initial(a.name)}</span>
                <span className="title-sm" style={{ flex: 1 }}>{a.name}</span>
                <span className="meta mono">{a.base}</span>
                <button type="button" className="link-btn" onClick={() => onRemove(a.id)} aria-label={`Remove ${a.name}`}>Remove</button>
              </div>
            ))}
          </section>
        )}

        <div className="connect-foot">
          <span className="meta" style={{ flex: 1, whiteSpace: "normal" }} aria-live="polite">
            {ready?.test && <>Test call: “{TEST_PROMPT}” {(ready.test.ms / 1000).toFixed(1)} s · OK · “{ready.test.text}”</>}
            {ready?.testError && <span className="error-line">Test call failed: {ready.testError}</span>}
            {ready && !ready.test && !ready.testError && <>Test call: “{TEST_PROMPT}” …</>}
          </span>
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="button" className="btn light" disabled={!ready?.test} onClick={() => ready && onConnect(ready.agent)}>
            Connect {ready?.card.name ?? "agent"}
          </button>
        </div>
      </div>
    </main>
  );
}
