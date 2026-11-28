import type { ActionPayload } from "@a2ui/web_core";
import type { Item } from "../lib/thread";
import { clock } from "../lib/look";
import { A2uiBlock } from "./A2uiBlock";
import { IconShield } from "./icons";

type ApprovalItem = Extract<Item, { kind: "approval" }>;

export function Approval({ item, busy, onAction, onDecide }: {
  item: ApprovalItem;
  busy: boolean;
  onAction: (a: ActionPayload) => void;
  onDecide: (approve: boolean) => void;
}) {
  const ap = item.approval;
  const title = `Approval: ${ap?.tool ?? "a tool call"}`;
  const decided = item.decision;
  return (
    <section className="approval" aria-label={title}>
      <div className="approval-head">
        <IconShield />
        <span className="t">{title}</span>
        {decided && (
          <span className={`decision ${decided}`}>
            {decided === "approved" ? "Approved" : "Rejected"} · {clock(item.decidedAt ?? item.at)}
          </span>
        )}
      </div>
      {decided || item.messages.length === 0 ? (
        <>
          {ap && <p className="msg-text">{ap.summary}</p>}
          {ap && (
            <dl>
              {Object.entries(ap.args).map(([k, v]) => (
                <div key={k} style={{ display: "contents" }}>
                  <dt>{k}</dt>
                  <dd className="mono">{typeof v === "string" ? v : JSON.stringify(v)}</dd>
                </div>
              ))}
            </dl>
          )}
          {!decided && (
            <div className="btn-row">
              <button type="button" className="btn primary" disabled={busy} onClick={() => onDecide(true)}>Approve</button>
              <button type="button" className="btn" disabled={busy} onClick={() => onDecide(false)}>Reject</button>
            </div>
          )}
        </>
      ) : (
        <A2uiBlock messages={item.messages} onAction={busy ? undefined : onAction} />
      )}
      <div className="foot">Code wrote this summary from the tool call. The model did not write it.</div>
    </section>
  );
}
