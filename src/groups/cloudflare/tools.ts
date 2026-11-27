// The Cloudflare DNS tool group: mail records only. Terraform owns the A,
// AAAA and CNAME records, so a type outside this list could start a fight
// with a Terraform apply.
import { readFileSync } from "node:fs";
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { approvedTool, holdsAll, type ToolGroupDef } from "../../group.ts";
import { HttpCloudflareDns, type CloudflareDns, type DnsRecord } from "./client.ts";
import { domain } from "../../schemas.ts";
import { result } from "../../result.ts";

const guidance = readFileSync(new URL("./guidance.md", import.meta.url), "utf8");

const RECORD_TYPES = ["MX", "TXT"] as const;

const recordName = z
  .string()
  .regex(
    /^(@|[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*)$/,
    "@ for the domain itself, or a lowercase name such as x._domainkey",
  )
  .describe("@ for the domain itself, or the name in front of the domain, for example x._domainkey");

function fqdn(name: string, dom: string): string {
  if (name === "@" || name === dom) return dom;
  return name.endsWith(`.${dom}`) ? name : `${name}.${dom}`;
}

// Cloudflare can return a TXT value in quotes and an MX host with a final dot.
function same(type: string, a: string, b: string): boolean {
  const norm = (s: string) =>
    type === "TXT" ? s.replace(/^"(.*)"$/s, "$1") : s.toLowerCase().replace(/\.$/, "");
  return norm(a) === norm(b);
}

const isSpf = (r: { type: string; content: string }) =>
  r.type === "TXT" && /^"?v=spf1(\s|"?$)/i.test(r.content);

function show(r: Pick<DnsRecord, "type" | "name" | "content" | "priority">): string {
  return `${r.type} ${r.name}${r.priority !== undefined && r.type === "MX" ? ` ${r.priority}` : ""} ${r.content}`;
}

function view(r: DnsRecord) {
  return {
    type: r.type,
    name: r.name,
    content: r.content,
    ...(r.type === "MX" ? { priority: r.priority } : {}),
    ttl: r.ttl === 1 ? "auto" : r.ttl,
    proxied: r.proxied ?? false,
    ...(r.comment ? { comment: r.comment } : {}),
  };
}

export const cloudflareGroup: ToolGroupDef<CloudflareDns> = {
  id: "cloudflare",
  name: "Cloudflare DNS",
  host: "api.cloudflare.com",
  keys: ["CLOUDFLARE_API_TOKEN"],
  setup: "Add the Cloudflare API token to the agent's secrets in Infisical, then restart the agent.",
  guidance,
  connect(ctx) {
    return holdsAll(ctx, this.keys) ? new HttpCloudflareDns(ctx.get("CLOUDFLARE_API_TOKEN")) : undefined;
  },
  tools(cf, shared): StructuredTool[] {
    const { policy } = shared;
    const { operator, agent } = policy;
    const comment = `Created by ${agent}, approved by ${operator}`;

    async function zoneFor(dom: string) {
      const zone = await cf.findZone(dom);
      if (!zone) {
        return `Not done: Cloudflare has no zone for ${dom} that ${agent}'s token can see. Give ${operator} the records to add by hand.`;
      }
      return zone;
    }

    const listRecords = tool(
      ({ domain: dom, type }) =>
        result(async () => {
          const zone = await zoneFor(dom);
          if (typeof zone === "string") return zone;
          const records = await cf.listRecords(zone.id);
          return records
            .filter((r) => r.name === dom || r.name.endsWith(`.${dom}`))
            .filter((r) => !type || r.type === type)
            .map(view);
        }),
      {
        name: "cloudflare_list_dns_records",
        description:
          "List the DNS records of a domain in Cloudflare, for example to check the MX, SPF and DKIM records.",
        schema: z.object({
          domain,
          type: z.enum(["A", "AAAA", "CNAME", "MX", "TXT", "CAA", "SRV", "NS"]).optional().describe("Only this record type"),
        }),
      },
    );

    const createRecord = approvedTool({
      name: "cloudflare_create_dns_record",
      operator,
      description: `Create one MX or TXT record in Cloudflare DNS, for example a record that MXroute needs. It cannot change or delete a record, and it makes no other record types. ${operator} must approve this action.`,
      schema: z
        .object({
          domain,
          type: z.enum(RECORD_TYPES).describe("MX or TXT"),
          name: recordName,
          content: z.string().min(1).max(2048).describe("For MX the mail server host name; for TXT the text, with no quotes"),
          priority: z.number().int().min(0).max(65535).optional().describe("MX only: the priority, for example 10"),
        })
        .refine((a) => (a.type === "MX") === (a.priority !== undefined), {
          message: "give a priority for an MX record, and no priority for a TXT record",
          path: ["priority"],
        }),
      prepare: async (args) => {
        const zone = await zoneFor(args.domain);
        if (typeof zone === "string") return { summary: "", early: zone };
        const name = fqdn(args.name, args.domain);
        const atName = (await cf.listRecords(zone.id)).filter((r) => r.name === name);

        const twin = atName.find(
          (r) =>
            r.type === args.type &&
            same(args.type, r.content, args.content) &&
            (args.type !== "MX" || r.priority === args.priority),
        );
        if (twin) return { summary: "", early: `Already in Cloudflare: ${show(twin)}. Nothing to do.` };

        const cname = atName.find((r) => r.type === "CNAME");
        if (cname) {
          return {
            summary: "",
            early: `Not done: ${name} is a CNAME (${cname.content}). No other record can be at that name. Tell ${operator}.`,
          };
        }

        const spf = isSpf(args) ? atName.find(isSpf) : undefined;
        if (spf) {
          return {
            summary: "",
            early: `Not done: ${name} already has an SPF record: ${spf.content}. A name must have one SPF record only, and you cannot change records. Give ${operator} both values to merge by hand.`,
          };
        }

        const now = atName.length ? atName.map(show).join("; ") : "none";
        return {
          summary: `Create in Cloudflare (zone ${zone.name}): ${show({ ...args, name })}. Records at that name now: ${now}`,
          data: { zoneId: zone.id, name },
        };
      },
      run: async (args, data) => {
        const created = await cf.createRecord(data.zoneId, {
          type: args.type,
          name: data.name,
          content: args.content,
          ...(args.type === "MX" ? { priority: args.priority } : {}),
          comment,
        });
        return `Created ${show(created)}.`;
      },
    });

    return [listRecords, createRecord];
  },
};
