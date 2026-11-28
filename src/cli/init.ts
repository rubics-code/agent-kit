// Makes a new agent folder from the template beside this package.
import { cpSync, existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = new URL("../../", import.meta.url);
const TEMPLATE = fileURLToPath(new URL("template/", root));

export const AGENT_NAME = /^[a-z][a-z0-9-]{0,39}$/;

export interface InitOptions {
  // The folder name and the package name, for example scout or ops-bot.
  name: string;
  // The new folder. It must not exist.
  dir: string;
  // Who approves the agent's actions. Default: "the operator".
  operator?: string | undefined;
}

// scout -> Scout, ops-bot -> Ops Bot.
export const displayName = (name: string) =>
  name
    .split("-")
    .filter(Boolean)
    .map((w) => w[0]!.toUpperCase() + w.slice(1))
    .join(" ");

// The same rule as runAgent's default env prefix.
export const envPrefix = (display: string) => display.toUpperCase().replace(/[^A-Z0-9]+/g, "_");

interface KitPackage {
  name: string;
  version: string;
  dependencies: Record<string, string>;
  devDependencies: Record<string, string>;
}

function kitPackage(): KitPackage {
  return JSON.parse(readFileSync(new URL("package.json", root), "utf8")) as KitPackage;
}

function packageJson(name: string, display: string, kit: KitPackage): string {
  const pick = (from: Record<string, string>, keys: string[]) => Object.fromEntries(keys.map((k) => [k, from[k]!]));
  // 0.0.0 is the version in the kit's own repo; a release sets the real one.
  const kitRange = kit.version === "0.0.0" ? "*" : `^${kit.version}`;
  return `${JSON.stringify(
    {
      name,
      version: "0.1.0",
      private: true,
      type: "module",
      description: `${display}, an agent on ${kit.name}`,
      engines: { node: ">=24" },
      scripts: {
        chat: "node src/main.ts",
        "type-check": "tsc --noEmit",
        test: "vitest run test/",
        eval: "vitest run evals/",
      },
      dependencies: { [kit.name]: kitRange, ...pick(kit.dependencies, ["langchain", "zod"]) },
      devDependencies: pick(kit.devDependencies, ["@types/node", "typescript", "vitest"]),
    },
    null,
    2,
  )}\n`;
}

// Writes the agent folder and returns the paths it made, relative to `dir`.
export function initAgent(opts: InitOptions): string[] {
  if (!AGENT_NAME.test(opts.name)) {
    throw new Error(`"${opts.name}" is not a valid agent name. Use lower case letters, digits and dashes, for example scout.`);
  }
  if (existsSync(opts.dir)) throw new Error(`${opts.dir} exists. Give a new folder.`);

  const display = displayName(opts.name);
  const values: Record<string, string> = {
    name: opts.name,
    Name: display,
    NAME: envPrefix(display),
    operator: opts.operator?.trim() || "the operator",
  };

  cpSync(TEMPLATE, opts.dir, { recursive: true });
  const made: string[] = [];
  for (const entry of readdirSync(opts.dir, { recursive: true, withFileTypes: true })) {
    if (!entry.isFile()) continue;
    let file = join(entry.parentPath, entry.name);
    // npm drops .gitignore from a package, so the template holds _gitignore.
    if (entry.name.startsWith("_")) {
      const dotted = join(entry.parentPath, `.${entry.name.slice(1)}`);
      renameSync(file, dotted);
      file = dotted;
    }
    const text = readFileSync(file, "utf8").replace(/\{\{(\w+)\}\}/g, (all, key: string) => values[key] ?? all);
    writeFileSync(file, text);
    made.push(file.slice(opts.dir.length + 1));
  }
  writeFileSync(join(opts.dir, "package.json"), packageJson(opts.name, display, kitPackage()));
  made.push("package.json");
  return made.sort();
}
