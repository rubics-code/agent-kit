import { ServiceError } from "./errors.ts";

export async function result(run: () => Promise<unknown>): Promise<string> {
  try {
    const value = await run();
    return typeof value === "string" ? value : JSON.stringify(value);
  } catch (err) {
    if (err instanceof ServiceError) return `Error: ${err.message}`;
    throw err;
  }
}

// The operator's name comes from the agent's policy (ADR-0003). The model
// never writes this copy; the gate decided the rejection.
export function rejected(reason: string, operator = "the operator"): string {
  return `Not done: ${operator} did not approve (${reason}). Do not try again unless ${operator} asks.`;
}
