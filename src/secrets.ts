import { randomInt } from "node:crypto";

// Where a new secret goes. It never goes into the conversation, so the model
// and the traces never hold it. The CLI prints it; the deployed agent will
// write it to Infisical.
export interface SecretSink {
  deliver(label: string, value: string): Promise<void>;
  // Where the secret goes, for the tool's reply. Default: the operator's terminal.
  where?: string;
}

const SETS = ["ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "-_.!"];
const ALL = SETS.join("");

export function generatePassword(length = 24): string {
  const chars = SETS.map((s) => s[randomInt(s.length)]!);
  while (chars.length < length) chars.push(ALL[randomInt(ALL.length)]!);
  for (let i = chars.length - 1; i > 0; i--) {
    const j = randomInt(i + 1);
    [chars[i], chars[j]] = [chars[j]!, chars[i]!];
  }
  return chars.join("");
}
