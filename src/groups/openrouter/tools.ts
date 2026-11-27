// The OpenRouter keys tool group. The spend limit is the agent policy's
// maximum; the schema rejects a higher one before the approval step.
import { readFileSync } from "node:fs";
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { approvedTool, holdsAll, type ToolGroupDef } from "../../group.ts";
import { HttpOpenRouterKeys, type ApiKey, type OpenRouterKeys } from "./client.ts";
import { NO_INFISICAL, deliverSecret, describePlace, storeAtFor } from "../../secret-delivery.ts";
import { result } from "../../result.ts";

const guidance = readFileSync(new URL("./guidance.md", import.meta.url), "utf8");

const hash = z
  .string()
  .regex(/^[A-Za-z0-9_-]{8,128}$/, "the hash of a key, from openrouter_list_keys")
  .describe("The key hash from openrouter_list_keys");

const limitReset = z.enum(["daily", "weekly", "monthly"]).describe("When the spend limit resets");

const usd = (n: number | null) => (n === null ? "no limit" : `$${n.toFixed(2)}`);

function view(k: ApiKey) {
  return {
    hash: k.hash,
    name: k.name,
    label: k.label,
    disabled: k.disabled,
    limit_usd: k.limit,
    limit_remaining_usd: k.limit_remaining,
    limit_reset: k.limit_reset,
    usage_month_usd: k.usage_monthly,
    usage_total_usd: k.usage,
    last_used_at: k.last_used_at,
    expires_at: k.expires_at,
  };
}

export const openrouterGroup: ToolGroupDef<OpenRouterKeys> = {
  id: "openrouter",
  name: "OpenRouter keys",
  host: "openrouter.ai",
  keys: ["OPENROUTER_MANAGEMENT_KEY"],
  setup: "Add the OpenRouter management key to the agent's secrets in Infisical, then restart the agent.",
  guidance,
  connect(ctx) {
    return holdsAll(ctx, this.keys) ? new HttpOpenRouterKeys(ctx.get("OPENROUTER_MANAGEMENT_KEY")) : undefined;
  },
  tools(keys, shared): StructuredTool[] {
    const { secrets, store, policy } = shared;
    const { operator, maxKeyLimitUsd } = policy;
    const storeAt = storeAtFor(policy);
    const limitUsd = z
      .number()
      .positive()
      .max(maxKeyLimitUsd)
      .describe(`Spend limit in USD, at most ${maxKeyLimitUsd}`);

    const getCredits = tool(() => result(() => keys.getCredits()), {
      name: "openrouter_get_credits",
      description: "Get the OpenRouter credits bought and used, in USD.",
      schema: z.object({}),
    });

    const listKeys = tool(
      ({ includeDisabled }) => result(async () => (await keys.listKeys({ includeDisabled })).map(view)),
      {
        name: "openrouter_list_keys",
        description: "List the OpenRouter API keys with their spend limits and usage in USD.",
        schema: z.object({
          includeDisabled: z.boolean().optional().describe("Also list disabled keys. Default false."),
        }),
      },
    );

    const createKey = approvedTool({
      name: "openrouter_create_key",
      operator,
      description: `Create an OpenRouter API key with a spend limit. The tool gives the key to ${operator} directly, or stores it in Infisical with storeAt; you never see it. ${operator} must approve this action.`,
      schema: z.object({
        name: z.string().min(1).max(100).describe("What the key is for, for example 'Acme website chatbot'"),
        limitUsd,
        limitReset: limitReset.optional().describe("When the spend limit resets. Default monthly."),
        expiresOn: z.iso.date().optional().describe("Last day the key works, YYYY-MM-DD (UTC). Default no expiry."),
        storeAt,
      }),
      prepare: async (args) => {
        if (args.storeAt && !store) return { summary: "", early: NO_INFISICAL };
        const reset = args.limitReset ?? "monthly";
        const expires = args.expiresOn ? ` until ${args.expiresOn}` : " with no expiry";
        const where = args.storeAt && store ? ` and store it in ${await describePlace(policy, store, args.storeAt)}` : "";
        return {
          summary: `Create the OpenRouter API key "${args.name}" (limit ${usd(args.limitUsd)} ${reset}${expires})${where}`,
          args: { ...args, limitReset: reset },
          data: { reset },
        };
      },
      run: async (args, data) => {
        const created = await keys.createKey({
          name: args.name,
          limit: args.limitUsd,
          limit_reset: data.reset,
          ...(args.expiresOn ? { expires_at: `${args.expiresOn}T23:59:59Z` } : {}),
        });
        const done = await deliverSecret({ policy, secrets, store, place: args.storeAt }, `OpenRouter key ${args.name}`, created.key);
        return `Created the key "${args.name}" (${created.data.label}, hash ${created.data.hash}). ${done}`;
      },
    });

    const updateKey = approvedTool({
      name: "openrouter_update_key",
      operator,
      description: `Change the spend limit of an OpenRouter API key, or disable or enable it. A disabled key stops at once. ${operator} must approve this action.`,
      schema: z
        .object({
          hash,
          limitUsd: limitUsd.optional(),
          limitReset: limitReset.optional(),
          disabled: z.boolean().optional().describe("true stops the key; false starts it again"),
        })
        .refine((a) => a.limitUsd !== undefined || a.limitReset !== undefined || a.disabled !== undefined, {
          message: "give at least one change: limitUsd, limitReset or disabled",
        }),
      prepare: async (args) => {
        const current = await keys.getKey(args.hash);
        const changes: string[] = [];
        if (args.limitUsd !== undefined) changes.push(`limit ${usd(current.limit)} to ${usd(args.limitUsd)}`);
        if (args.limitReset !== undefined) changes.push(`reset ${current.limit_reset ?? "never"} to ${args.limitReset}`);
        if (args.disabled !== undefined) changes.push(args.disabled ? "disable it" : "enable it");
        return { summary: `Change the OpenRouter API key "${current.name}" (${current.label}): ${changes.join(", ")}` };
      },
      run: async (args) =>
        view(
          await keys.updateKey(args.hash, {
            ...(args.limitUsd !== undefined ? { limit: args.limitUsd } : {}),
            ...(args.limitReset !== undefined ? { limit_reset: args.limitReset } : {}),
            ...(args.disabled !== undefined ? { disabled: args.disabled } : {}),
          }),
        ),
    });

    return [getCredits, listKeys, createKey, updateKey];
  },
};
