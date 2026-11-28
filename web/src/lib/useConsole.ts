import { useCallback, useEffect, useRef, useState } from "react";
import type { ActionPayload } from "@a2ui/web_core";
import { A2AClient, A2UI_MIME, endpointFor, fetchCard, newId, rendersA2UI, userMessage, type Part } from "./a2a";
import { catalogIds } from "./catalogs";
import { agentId, loadAgents, loadThreads, saveAgents, saveThreads, upsertAgent, type Agent } from "./storage";
import { addError, addUser, applyEvent, decide, newThread, type Thread } from "./thread";

export type Route = { name: "thread" | "dashboard" | "connect"; threadId?: string };

export function parseRoute(hash: string): Route {
  const [, name, id] = hash.replace(/^#/, "").split("/");
  if (name === "dashboard" || name === "connect") return { name };
  if (name === "t" && id) return { name: "thread", threadId: id };
  return { name: "thread" };
}

const SELECTED = "console.selected.v1";

// useConsole holds the agents, the threads, and the A2A calls.
export function useConsole() {
  const [agents, setAgents] = useState<Agent[]>(() => loadAgents());
  const [threads, setThreads] = useState<Thread[]>(() => loadThreads());
  const [selected, setSelected] = useState<string | undefined>(() => localStorage.getItem(SELECTED) ?? undefined);
  const [route, setRoute] = useState<Route>(() => parseRoute(location.hash));
  const [draftId, setDraftId] = useState(() => newId());
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const threadsRef = useRef(threads);
  threadsRef.current = threads;

  useEffect(() => { saveThreads(threads); }, [threads]);
  useEffect(() => { saveAgents(agents); }, [agents]);
  useEffect(() => {
    if (selected) localStorage.setItem(SELECTED, selected);
  }, [selected]);
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash));
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, []);

  // The agent that serves this page connects itself.
  useEffect(() => {
    const base = location.origin;
    fetchCard(base)
      .then((card) => {
        const a: Agent = { id: agentId(base), name: card.name, base, endpoint: endpointFor(card, base, base), description: card.description, a2ui: rendersA2UI(card), connectedAt: Date.now() };
        setAgents((list) => upsertAgent(list, a));
        setSelected((s) => s ?? a.id);
      })
      .catch(() => {});
  }, []);

  const agent = agents.find((a) => a.id === selected) ?? agents[0];
  const routed = route.threadId ? threads.find((t) => t.id === route.threadId) : undefined;
  const thread = routed ?? (route.threadId ? undefined : threads.find((t) => t.id === draftId));
  const threadId = routed?.id ?? draftId;

  const update = useCallback((id: string, f: (t: Thread) => Thread) => {
    setThreads((list) => list.map((t) => (t.id === id ? f(t) : t)));
  }, []);

  const run = useCallback(
    async (a: Agent, id: string, parts: Part[], taskId?: string) => {
      setBusy((b) => ({ ...b, [id]: true }));
      try {
        const client = new A2AClient(a.endpoint);
        for await (const ev of client.stream(userMessage(id, parts, a.a2ui ? catalogIds : [], taskId))) {
          update(id, (t) => applyEvent(t, ev, Date.now()));
        }
      } catch (e) {
        update(id, (t) => addError(t, `The agent did not answer: ${e instanceof Error ? e.message : String(e)}`, Date.now()));
      } finally {
        setBusy((b) => ({ ...b, [id]: false }));
      }
    },
    [update],
  );

  const send = useCallback(
    (text: string, voice = false) => {
      if (!agent) return;
      const id = threadId;
      const now = Date.now();
      setThreads((list) => {
        const t = list.find((x) => x.id === id) ?? newThread(id, agent.id, now);
        const rest = list.filter((x) => x.id !== id);
        return [...rest, addUser(t, text, now, voice)];
      });
      if (!route.threadId) location.hash = `#/t/${id}`;
      setDraftId(newId());
      const pending = threadsRef.current.find((x) => x.id === id)?.pendingTaskId;
      void run(agent, id, [{ text }], pending);
    },
    [agent, threadId, route.threadId, run],
  );

  const action = useCallback(
    (a: ActionPayload) => {
      const t = threadsRef.current.find((x) => x.id === threadId);
      if (!agent || !t) return;
      const confirmationId = a.context.confirmationId;
      if (typeof confirmationId === "string" && (a.name === "kit.approve" || a.name === "kit.reject")) {
        update(t.id, (x) => decide(x, confirmationId, a.name === "kit.approve" ? "approved" : "rejected", Date.now()));
      }
      const msg = { version: "1.0", action: { name: a.name, surfaceId: a.surfaceId, sourceComponentId: a.sourceComponentId, timestamp: a.timestamp, context: a.context } };
      void run(agent, t.id, [{ data: [msg], metadata: { mimeType: A2UI_MIME } }], t.pendingTaskId);
    },
    [agent, threadId, run, update],
  );

  // decideDirect answers a confirmation without A2UI, for an agent that sent no card.
  const decideDirect = useCallback(
    (confirmationId: string, approve: boolean) => {
      const t = threadsRef.current.find((x) => x.id === threadId);
      const item = t?.items.find((i) => i.kind === "approval" && i.confirmationId === confirmationId);
      if (!agent || !t || !item || item.kind !== "approval") return;
      update(t.id, (x) => decide(x, confirmationId, approve ? "approved" : "rejected", Date.now()));
      const part: Part = {
        data: { id: confirmationId, name: "adk_request_confirmation", response: { confirmed: approve, payload: { argsHash: item.approval?.argsHash } } },
        metadata: { adk_type: "function_response" },
      };
      void run(agent, t.id, [part], t.pendingTaskId);
    },
    [agent, threadId, run, update],
  );

  const newThreadNow = useCallback(() => {
    setDraftId(newId());
    location.hash = "#/";
  }, []);

  const selectAgent = useCallback((id: string) => {
    setSelected(id);
    setDraftId(newId());
    location.hash = "#/";
  }, []);

  const connect = useCallback((a: Agent) => {
    setAgents((list) => upsertAgent(list, a));
    setSelected(a.id);
    setDraftId(newId());
    location.hash = "#/";
  }, []);

  const removeAgent = useCallback((id: string) => {
    setAgents((list) => list.filter((a) => a.id !== id));
    setSelected((s) => (s === id ? undefined : s));
  }, []);

  const busyThread = Object.entries(busy).find(([, v]) => v)?.[0];
  return { agents, setAgents, threads, agent, thread, threadId, route, busy: !!busy[threadId], busyThread, send, action, decideDirect, newThread: newThreadNow, selectAgent, setSelected, connect, removeAgent };
}
