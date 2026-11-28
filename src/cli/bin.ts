#!/usr/bin/env node
// npx @rubics-code/agent-kit init <name> [--dir <folder>] [--operator <name>]
import { execFileSync } from "node:child_process";
import { relative, resolve } from "node:path";
import { parseArgs } from "node:util";
import { initAgent } from "./init.ts";

const USAGE = `Usage: agent-kit init <name> [--dir <folder>] [--operator <name>]

  <name>       lower case letters, digits and dashes, for example scout
  --dir        the new folder (default: agents/<name>)
  --operator   who approves the agent's actions (default: your git user.name)
`;

function gitUser(): string | undefined {
  try {
    return execFileSync("git", ["config", "user.name"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim().split(/\s+/)[0];
  } catch {
    return undefined;
  }
}

const { values, positionals } = parseArgs({
  allowPositionals: true,
  options: { dir: { type: "string" }, operator: { type: "string" }, help: { type: "boolean", short: "h" } },
});
const [command, name] = positionals;

if (values.help || command !== "init" || !name) {
  process.stdout.write(USAGE);
  process.exit(values.help ? 0 : 1);
}

const dir = resolve(values.dir ?? `agents/${name}`);
try {
  const files = initAgent({ name, dir, operator: values.operator ?? gitUser() });
  const rel = relative(process.cwd(), dir) || ".";
  process.stdout.write(
    `Made ${rel} (${files.length} files).\n\nNext:\n  cd ${rel}\n  npm install\n  OPENROUTER_API_KEY=... npm run chat\n\nRead ${rel}/README.md for the tool groups, Infisical and container mode.\n`,
  );
} catch (err) {
  process.stderr.write(`${err instanceof Error ? err.message : String(err)}\n`);
  process.exit(1);
}
