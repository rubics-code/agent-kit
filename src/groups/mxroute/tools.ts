// The MXroute tool group. One definition carries everything the rest of the
// system needs to know: its tools, keys, host, setup hint and guidance.
import { readFileSync } from "node:fs";
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { approvedTool, holdsAll, type ToolGroupDef } from "../../group.ts";
import { HttpMxroute, type Mxroute } from "./client.ts";
import { generatePassword } from "../../secrets.ts";
import { domain } from "../../schemas.ts";
import { NO_INFISICAL, deliverSecret, describePlace, storeAtFor } from "../../secret-delivery.ts";
import { result } from "../../result.ts";

const guidance = readFileSync(new URL("./guidance.md", import.meta.url), "utf8");

// MXroute: starts with a letter or digit; letters, digits, '.', '_', '-'; at most 64.
const localPart = z
  .string()
  .regex(/^[a-z0-9][a-z0-9._-]{0,63}$/, "a lowercase local part, for example hello")
  .describe("The part before the @, lowercase, for example hello");

export const mxrouteGroup: ToolGroupDef<Mxroute> = {
  id: "mxroute",
  name: "MXroute",
  host: "api.mxroute.com",
  keys: ["MXROUTE_SERVER", "MXROUTE_USERNAME", "MXROUTE_API_KEY"],
  setup: "Add the MXroute keys to the agent's secrets in Infisical, then restart the agent.",
  guidance,
  connect(ctx) {
    if (!holdsAll(ctx, this.keys)) return undefined;
    return new HttpMxroute({
      server: ctx.get("MXROUTE_SERVER"),
      username: ctx.get("MXROUTE_USERNAME"),
      apiKey: ctx.get("MXROUTE_API_KEY"),
    });
  },
  tools(mx, shared): StructuredTool[] {
    const { secrets, store, policy } = shared;
    const { operator } = policy;
    const storeAt = storeAtFor(policy);
    // Where a stored password would go, for the approval text.
    const where = async (place: { path: string; name: string } | undefined) =>
      place && store ? ` and store the password in ${await describePlace(policy, store, place)}` : "";

    const listDomains = tool(() => result(() => mx.listDomains()), {
      name: "mxroute_list_domains",
      description: "List the domains on the MXroute account.",
      schema: z.object({}),
    });

    const listMailboxes = tool(({ domain: d }) => result(() => mx.listEmailAccounts(d)), {
      name: "mxroute_list_mailboxes",
      description: "List the mailboxes of a domain, with quota (MB), usage (MB) and daily send limit.",
      schema: z.object({ domain }),
    });

    const listForwarders = tool(({ domain: d }) => result(() => mx.listForwarders(d)), {
      name: "mxroute_list_forwarders",
      description: "List the forwarders of a domain.",
      schema: z.object({ domain }),
    });

    const getDnsRecords = tool(({ domain: d }) => result(() => mx.getDnsInfo(d)), {
      name: "mxroute_get_dns_records",
      description: "Get the DNS records (MX, SPF, DKIM) that a domain on MXroute needs at its DNS host.",
      schema: z.object({ domain }),
    });

    const getVerificationRecord = tool(
      ({ domain: d }) =>
        result(async () => {
          const v = await mx.getVerificationKey();
          return { type: v.record.type, name: `${v.record.name}.${d}`, value: v.record.value };
        }),
      {
        name: "mxroute_get_verification_record",
        description:
          "Get the TXT record that must exist in a domain's DNS before MXroute accepts the domain. Use it before mxroute_add_domain.",
        schema: z.object({ domain }),
      },
    );

    const addDomain = approvedTool({
      name: "mxroute_add_domain",
      operator,
      description: `Add a domain to MXroute. The verification TXT record must be live first. ${operator} must approve this action.`,
      schema: z.object({ domain }),
      prepare: async (args) => ({ summary: `Add the domain ${args.domain} to MXroute` }),
      run: async (args) => {
        await mx.addDomain(args.domain);
        return `Added ${args.domain}. Next, get its DNS records with mxroute_get_dns_records.`;
      },
    });

    const createMailbox = approvedTool({
      name: "mxroute_create_mailbox",
      operator,
      description: `Create a mailbox. The tool makes the password and gives it to ${operator} directly, or stores it in Infisical with storeAt; never ask for one. ${operator} must approve this action.`,
      schema: z.object({
        domain,
        username: localPart,
        quotaMb: z.number().int().min(0).max(102400).optional().describe("Storage quota in MB. 0 = unlimited. Default 1024."),
        dailySendLimit: z.number().int().min(1).max(9600).optional().describe("Emails per day. Default 9600."),
        storeAt,
      }),
      prepare: async (args) => {
        if (args.storeAt && !store) return { summary: "", early: NO_INFISICAL };
        const quota = args.quotaMb ?? 1024;
        const limit = args.dailySendLimit ?? 9600;
        return {
          summary: `Create the mailbox ${args.username}@${args.domain} (quota ${quota === 0 ? "unlimited" : `${quota} MB`}, send limit ${limit} per day)${await where(args.storeAt)}`,
          args: { ...args, quotaMb: quota, dailySendLimit: limit },
          data: { quota, limit },
        };
      },
      run: async (args, data) => {
        const password = generatePassword();
        await mx.createEmailAccount(args.domain, {
          username: args.username,
          password,
          quota: data.quota,
          limit: data.limit,
        });
        const label = `${args.username}@${args.domain}`;
        return `Created ${label}. ${await deliverSecret({ policy, secrets, store, place: args.storeAt }, label, password)}`;
      },
    });

    const resetPassword = approvedTool({
      name: "mxroute_reset_mailbox_password",
      operator,
      description: `Give a mailbox a new password. The tool makes it and gives it to ${operator} directly, or stores it in Infisical with storeAt. ${operator} must approve this action.`,
      schema: z.object({ domain, username: localPart, storeAt }),
      prepare: async (args) => ({
        summary: `Set a new password for ${args.username}@${args.domain}. The old password stops working${await where(args.storeAt)}.`,
        early: args.storeAt && !store ? NO_INFISICAL : undefined,
      }),
      run: async (args) => {
        const password = generatePassword();
        await mx.setEmailPassword(args.domain, args.username, password);
        const label = `${args.username}@${args.domain}`;
        return `New password set for ${label}. ${await deliverSecret({ policy, secrets, store, place: args.storeAt }, label, password)}`;
      },
    });

    const createForwarder = approvedTool({
      name: "mxroute_create_forwarder",
      operator,
      description: `Create a forwarder. A forwarder to Gmail, Yahoo or AOL turns on Expert Spam Filtering for the domain. ${operator} must approve this action.`,
      schema: z.object({
        domain,
        alias: localPart,
        destinations: z.array(z.email()).min(1).max(10).describe("Addresses that get the mail"),
      }),
      prepare: async (args) => ({
        summary: `Forward ${args.alias}@${args.domain} to ${args.destinations.join(", ")}`,
      }),
      run: async (args) => {
        await mx.createForwarder(args.domain, { alias: args.alias, destinations: args.destinations });
        return `Created the forwarder ${args.alias}@${args.domain}.`;
      },
    });

    return [
      listDomains,
      listMailboxes,
      listForwarders,
      getDnsRecords,
      getVerificationRecord,
      addDomain,
      createMailbox,
      resetPassword,
      createForwarder,
    ];
  },
};
