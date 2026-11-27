// The Infisical secret tool group. Its paths come from the agent's policy:
// the readable and writable roots are policy data, and the identity's Infisical
// role is the second limit behind them.
import { readFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { tool } from "langchain";
import type { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { approvedTool, type EnableEnv, type ToolGroupDef } from "../../group.ts";
import type { SecretPlace, SecretStore } from "../../secret-store.ts";
import { describePlace, placeText } from "../../secret-delivery.ts";
import { result } from "../../result.ts";

const guidance = readFileSync(new URL("./guidance.md", import.meta.url), "utf8");

export const infisicalGroup: ToolGroupDef<SecretStore> = {
  id: "infisical",
  name: "Infisical",
  // The real host is the agent's Infisical deployment; the agent overrides it
  // at assembly time (review below reads env.host).
  host: "infisical",
  keys: [],
  setup: "Put the agent's Infisical identity in its .env file and start it with the identity script.",
  guidance,
  review(env: EnableEnv) {
    if (env.vaultHosts) {
      const on = !!env.host && env.vaultHosts.has(env.host);
      return { enabled: on, missing: on || !env.host ? [] : [env.host] };
    }
    // Identity mode: the store exists, so the tools are on. Without it, the
    // identity's own keys are what is missing.
    return {
      enabled: env.hasStore,
      missing: env.hasStore ? [] : ["INFISICAL_CLIENT_ID", "INFISICAL_CLIENT_SECRET"],
    };
  },
  // The store is the service. The agent makes it from its own Infisical config.
  connect(ctx) {
    return ctx.store;
  },
  tools(store, shared): StructuredTool[] {
    const { policy } = shared;
    const { operator } = policy;
    const { readablePath, writablePath } = policy.infisical;

    const listFolders = tool(({ path }) => result(() => store.listFolders(path)), {
      name: "infisical_list_folders",
      description: "List the folders in an Infisical folder, for example /agents to see the agents.",
      schema: z.object({ path: readablePath }),
    });

    const listNames = tool(({ path }) => result(() => store.listSecretNames(path)), {
      name: "infisical_list_secret_names",
      description: "List the secret names in an Infisical folder. You never see a value.",
      schema: z.object({ path: readablePath }),
    });

    const createRandom = approvedTool({
      name: "infisical_create_random_secret",
      operator,
      description: `Make a new random secret, for example an API_SERVER_KEY, and store it in Infisical. It does not replace a secret that exists. ${operator} must approve this action.`,
      schema: z.object({
        path: writablePath,
        name: z
          .string()
          .regex(/^[A-Z][A-Z0-9_]{0,127}$/, "upper case with underscores, for example API_SERVER_KEY")
          .describe("Secret name, upper case with underscores, for example API_SERVER_KEY"),
        bytes: z.number().int().min(16).max(64).optional().describe("Random bytes. Default 32."),
        format: z.enum(["hex", "base64url"]).optional().describe("Default hex."),
      }),
      prepare: async (args) => {
        const place: SecretPlace = { path: args.path, name: args.name };
        const { exists } = await store.check(place);
        if (exists) {
          return {
            summary: "",
            early: `Not done: ${placeText(policy, place)} already exists. You cannot replace a value with a random one. Tell ${operator}.`,
          };
        }
        const bytes = args.bytes ?? 32;
        const format = args.format ?? "hex";
        return {
          summary: `Make a random secret (${bytes} bytes, ${format}) and store it in ${await describePlace(policy, store, place)}`,
          args: { ...args, bytes, format },
          data: { bytes, format },
        };
      },
      run: async (args, data) => {
        await store.put({ path: args.path, name: args.name }, randomBytes(data.bytes).toString(data.format));
        return `Stored a new random secret in ${placeText(policy, { path: args.path, name: args.name })}. You do not have it.`;
      },
    });

    return [listFolders, listNames, createRandom];
  },
};
