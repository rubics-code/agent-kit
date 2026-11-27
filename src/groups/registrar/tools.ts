// The Cloudflare Registrar tool group: search, live check, and a gated
// registration. The price in the approval comes from a live check in the
// same call, never from the model.
import { readFileSync } from "node:fs";
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { approvedTool, holdsAll, type ToolGroupDef } from "../../group.ts";
import { domain } from "../../schemas.ts";
import { result } from "../../result.ts";
import { HttpCloudflareRegistrar, type CloudflareRegistrar, type DomainOffer, type RegistrationStatus } from "./client.ts";

const guidance = readFileSync(new URL("./guidance.md", import.meta.url), "utf8");

// Extensions that the API cannot register. The dashboard can.
export const DASHBOARD_ONLY = ["giving", "mom", "inc", "lol", "sh", "link", "cc", "new"] as const;

const POLLS = 10;

function view(o: DomainOffer) {
  return {
    name: o.name,
    registrable: o.registrable,
    ...(o.tier ? { tier: o.tier } : {}),
    ...(o.pricing ? { pricing: o.pricing } : {}),
    ...(o.reason ? { reason: o.reason } : {}),
  };
}

export const registrarGroup: ToolGroupDef<CloudflareRegistrar> = {
  id: "registrar",
  name: "Cloudflare Registrar",
  host: "api.cloudflare.com",
  keys: ["CLOUDFLARE_API_TOKEN"],
  setup:
    "Give the Cloudflare API token Registrar permission, and add a default payment method and a default contact in the Cloudflare dashboard. Then restart the agent.",
  guidance,
  connect(ctx) {
    return holdsAll(ctx, this.keys) ? new HttpCloudflareRegistrar(ctx.get("CLOUDFLARE_API_TOKEN")) : undefined;
  },
  tools(reg, shared): StructuredTool[] {
    const { operator } = shared.policy;
    const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
    const dashboard = (d: string, why: string) =>
      `Not done: the API cannot register ${d} (${why}). Tell ${operator} to register it in the Cloudflare dashboard.`;

    const search = tool(
      ({ query, limit }) => result(async () => (await reg.search(query, limit)).map(view)),
      {
        name: "cloudflare_search_domains",
        description:
          "Search Cloudflare Registrar for domains that match a word or a name. The availability is advisory: the register tool checks it again.",
        schema: z.object({
          query: z.string().min(1).max(100).describe("A word, a phrase or a domain name"),
          limit: z.number().int().min(1).max(50).optional().describe("The maximum number of results, default 20"),
        }),
      },
    );

    const check = tool(
      ({ domains }) => result(async () => (await reg.check(domains)).map(view)),
      {
        name: "cloudflare_check_domains",
        description: "Check with the registry, now, if domains are available, and get their prices.",
        schema: z.object({ domains: z.array(domain).min(1).max(20) }),
      },
    );

    function report(d: string, s: RegistrationStatus): string {
      switch (s.state) {
        case "succeeded":
          return `Registered ${d}${s.context?.registration?.expires_at ? `, expires ${s.context.registration.expires_at}` : ""}. Auto-renew is off. Terraform must add the DNS zone before mail records.`;
        case "action_required":
          return `Not finished: Cloudflare needs an action for ${d}: ${s.context?.action ?? "unknown"}${s.context?.confirmation_sent_to ? ` (sent to ${s.context.confirmation_sent_to})` : ""}. Tell ${operator}. Do not register again.`;
        case "failed":
          return `Failed: Cloudflare did not register ${d}: ${s.error?.code ?? ""} ${s.error?.message ?? ""}`.trim();
        case "blocked":
          return `Blocked: Cloudflare blocked the registration of ${d}. Tell ${operator} to look in the dashboard. Do not register again.`;
        default:
          return `Still ${s.state}: Cloudflare has not finished ${d}. Tell ${operator} to look in the dashboard later. Do not register again.`;
      }
    }

    const register = approvedTool({
      name: "cloudflare_register_domain",
      operator,
      description: `Register one domain in Cloudflare for one year. This costs money and has no refund. ${operator} must approve this action.`,
      schema: z.object({ domain }),
      prepare: async ({ domain: d }) => {
        const ext = d.split(".").pop()!;
        if ((DASHBOARD_ONLY as readonly string[]).includes(ext)) return { summary: "", early: dashboard(d, `.${ext}`) };
        const [offer] = await reg.check([d]);
        if (!offer?.registrable || !offer.pricing) {
          if (offer?.reason === "domain_premium" || offer?.tier === "premium") return { summary: "", early: dashboard(d, "premium") };
          if (offer?.reason?.startsWith("extension_not_supported")) return { summary: "", early: dashboard(d, offer.reason) };
          return { summary: "", early: `Not done: ${d} is not available (${offer?.reason ?? "unknown"}).` };
        }
        if (offer.tier === "premium") return { summary: "", early: dashboard(d, "premium") };
        const p = offer.pricing;
        return {
          summary: `Register ${d} in Cloudflare for 1 year: ${p.registration_cost} ${p.currency} now, renewal ${p.renewal_cost} ${p.currency} a year. Auto-renew off, WHOIS redaction on, default contact. No refund.`,
          // The gate hashes the price too, so an approval cannot cover a changed price.
          args: { domain: d, currency: p.currency, registration_cost: p.registration_cost },
        };
      },
      run: async ({ domain: d }) => {
        let s = await reg.register(d);
        for (let i = 0; i < POLLS && (s.state === "pending" || s.state === "in_progress"); i++) {
          await wait(reg.pollDelayMs ?? 3000);
          s = await reg.status(d);
        }
        return report(d, s);
      },
    });

    return [search, check, register];
  },
};
