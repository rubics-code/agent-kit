// The Kit's A2UI catalog: a few components in the console's own look. The
// agent sends these for approval cards and dashboard cards.
import { z } from "zod";
import { createComponentImplementation } from "@a2ui/react";
import { Catalog } from "@a2ui/web_core";
import { CommonSchemas } from "@a2ui/web_core/v1_0";
import { IconShield } from "../components/icons";

export const KIT_CATALOG = "https://github.com/rubics-code/agent-kit/catalogs/kit/v1";

const Entry = z.object({ label: z.string(), value: z.string(), mono: z.boolean().optional() });
const Tone = z.enum(["ok", "warn", "idle", "bad"]);

export const CardApi = {
  name: "Card",
  schema: z.object({
    title: CommonSchemas.DynamicString,
    meta: CommonSchemas.DynamicString.optional(),
    tag: z.string().optional(),
    child: CommonSchemas.ComponentId.optional(),
    footer: CommonSchemas.DynamicString.optional(),
  }),
};

export const StackApi = {
  name: "Stack",
  schema: z.object({ children: CommonSchemas.ChildList, gap: z.number().optional() }),
};

export const TextApi = {
  name: "Text",
  schema: z.object({ text: CommonSchemas.DynamicString, tone: z.enum(["body", "muted", "mono", "title"]).optional() }),
};

export const TableApi = {
  name: "Table",
  schema: z.object({ columns: z.array(z.string()), rows: z.array(z.array(z.string())), mono: z.array(z.number()).optional() }),
};

export const StatusListApi = {
  name: "StatusList",
  schema: z.object({ items: z.array(z.object({ label: z.string(), state: z.string(), tone: Tone.optional() })) }),
};

export const FieldsApi = {
  name: "Fields",
  schema: z.object({ entries: z.array(Entry) }),
};

export const ButtonApi = {
  name: "Button",
  schema: z.object({ label: z.string(), action: CommonSchemas.Action, variant: z.enum(["primary", "default", "light"]).optional() }),
};

export const ApprovalApi = {
  name: "Approval",
  schema: z.object({
    tool: z.string(),
    summary: z.string(),
    entries: z.array(Entry).optional(),
    argsHash: z.string(),
    approve: CommonSchemas.Action,
    reject: CommonSchemas.Action,
  }),
};

const toneColor: Record<string, string> = { ok: "#4CC38A", warn: "#E5B94C", idle: "#8A8E98", bad: "#F0616D" };

const Card = createComponentImplementation(CardApi, ({ props, buildChild }) => (
  <section className="kit-card">
    <div className="kit-card-head">
      <span className="kit-card-title">{props.title}</span>
      {props.meta && <span className="meta">{props.meta}</span>}
      {props.tag && <span className="kit-tag">{props.tag}</span>}
    </div>
    {props.child && <div className="kit-card-body">{buildChild(props.child)}</div>}
    {props.footer && <div className="kit-card-foot">{props.footer}</div>}
  </section>
));

const Stack = createComponentImplementation(StackApi, ({ props, buildChild }) => (
  <div className="kit-stack" style={{ gap: props.gap ?? 10 }}>
    {(props.children as unknown as string[] | undefined)?.map((id) => <div key={id}>{buildChild(id)}</div>)}
  </div>
));

const Text = createComponentImplementation(TextApi, ({ props }) => (
  <p className={`kit-text ${props.tone ?? "body"}`}>{props.text}</p>
));

const Table = createComponentImplementation(TableApi, ({ props }) => (
  <div className="kit-table-wrap">
    <table className="kit-table">
      <thead>
        <tr>{props.columns.map((c) => <th key={c} scope="col">{c}</th>)}</tr>
      </thead>
      <tbody>
        {props.rows.map((r, i) => (
          <tr key={i}>{r.map((v, j) => <td key={j} className={props.mono?.includes(j) ? "mono" : undefined}>{v}</td>)}</tr>
        ))}
      </tbody>
    </table>
  </div>
));

const StatusList = createComponentImplementation(StatusListApi, ({ props }) => (
  <ul className="kit-status">
    {props.items.map((it, i) => (
      <li key={i}>
        <span className="kit-dot" style={{ background: toneColor[it.tone ?? "idle"] }} aria-hidden="true" />
        <span className="kit-status-label">{it.label}</span>
        <span className="meta">{it.state}</span>
      </li>
    ))}
  </ul>
));

const Fields = createComponentImplementation(FieldsApi, ({ props }) => <FieldList entries={props.entries} />);

function FieldList({ entries }: { entries: { label: string; value: string; mono?: boolean }[] }) {
  return (
    <dl className="kit-fields">
      {entries.map((e, i) => (
        <div key={i} style={{ display: "contents" }}>
          <dt>{e.label}</dt>
          <dd className={e.mono ? "mono" : undefined}>{e.value}</dd>
        </div>
      ))}
    </dl>
  );
}

const Button = createComponentImplementation(ButtonApi, ({ props }) => (
  <button type="button" className={`btn ${props.variant ?? "default"}`} onClick={() => props.action?.()}>{props.label}</button>
));

const Approval = createComponentImplementation(ApprovalApi, ({ props }) => (
  <section className="approval" aria-label={`Approval: ${props.tool}`}>
    <div className="approval-head">
      <IconShield />
      <span className="t">Approval: {props.tool}</span>
    </div>
    <p className="msg-text">{props.summary}</p>
    {props.entries && props.entries.length > 0 && <FieldList entries={props.entries} />}
    <div className="btn-row">
      <button type="button" className="btn primary" onClick={() => props.approve?.()}>Approve</button>
      <button type="button" className="btn" onClick={() => props.reject?.()}>Reject</button>
    </div>
    <div className="foot">
      Code wrote this summary from the tool call. The model did not write it. <span className="mono" title="The approval binds to this hash of the arguments.">{props.argsHash.slice(0, 19)}…</span>
    </div>
  </section>
));

export const kitCatalog = new Catalog(KIT_CATALOG, "1.0", [Card, Stack, Text, Table, StatusList, Fields, Button, Approval], []);
