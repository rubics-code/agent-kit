// Delivering a new secret to the operator without the model ever seeing it:
// into Infisical with storeAt, or to the operator's terminal. Any group that
// makes a secret (mailbox passwords, API keys) uses these helpers.
import type { AgentPolicy } from "./policy.ts";
import { generatePassword } from "./secrets.ts";
import type { SecretPlace, SecretStore } from "./secret-store.ts";
import type { SecretSink } from "./secrets.ts";
import { z } from "zod";

export const secretName = z
  .string()
  .regex(/^[A-Z][A-Z0-9_]{0,127}$/, "upper case with underscores, for example API_SERVER_KEY")
  .describe("Secret name, upper case with underscores, for example API_SERVER_KEY");

// The optional storeAt argument, built from the agent's writable-path policy.
export function storeAtFor(policy: AgentPolicy) {
  return z
    .object({
      path: policy.infisical.writablePath,
      name: secretName,
    })
    .optional()
    .describe(
      `Optional. The Infisical folder and name to store the new secret in. Without it, the secret goes to ${policy.operator}'s terminal.`,
    );
}

export function placeText(policy: AgentPolicy, p: SecretPlace) {
  return `Infisical ${policy.infisical.envLabel(p.path)} ${p.path}/${p.name}`;
}

// The approval text for a write into Infisical: says when it replaces a value or makes a folder.
export async function describePlace(policy: AgentPolicy, store: SecretStore, p: SecretPlace): Promise<string> {
  const { folderExists, exists } = await store.check(p);
  if (exists) return `${placeText(policy, p)} (REPLACES the current value)`;
  return `${placeText(policy, p)} (new${folderExists ? "" : `, makes the folder ${p.path}`})`;
}

// Puts a new secret where the operator asked. If Infisical fails after the
// secret exists, the secret goes to the terminal so that it is not lost.
export async function deliverSecret(
  opts: { policy: AgentPolicy; secrets: SecretSink; store?: SecretStore | undefined; place?: SecretPlace | undefined },
  label: string,
  value: string,
): Promise<string> {
  const where = opts.secrets.where ?? `${opts.policy.operator}'s terminal`;
  if (!opts.place || !opts.store) {
    await opts.secrets.deliver(label, value);
    return `It went directly to ${where}. You do not have it.`;
  }
  try {
    await opts.store.put(opts.place, value);
    return `It is stored in ${placeText(opts.policy, opts.place)}. You do not have it.`;
  } catch (err) {
    await opts.secrets.deliver(label, value);
    const why = err instanceof Error ? err.message : String(err);
    return `Storing it in Infisical failed (${why}), so it went to ${where} instead. You do not have it.`;
  }
}

export const NO_INFISICAL =
  "Not done: Infisical is not set up for you, so you cannot use storeAt. Call again without it.";
