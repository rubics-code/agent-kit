// Proves that the published package works on its own: pack (prepack builds
// dist/), install the tarball into an empty folder with npm, run a consumer
// from plain Node, and type-check a consumer file.
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const work = mkdtempSync(join(tmpdir(), "agent-kit-pack-"));
const run = (cmd: string, args: string[], cwd: string) => execFileSync(cmd, args, { cwd, stdio: "inherit" });

try {
  run("npm", ["pack", "--silent", "--pack-destination", work], root);
  const tarball = readdirSync(work).find((f) => f.endsWith(".tgz"));
  if (!tarball) throw new Error("npm pack made no tarball");

  const app = join(work, "app");
  run("mkdir", ["-p", app], work);
  writeFileSync(join(app, "package.json"), JSON.stringify({ name: "consumer", private: true, type: "module" }));
  run("npm", ["install", "--no-audit", "--no-fund", "--silent", join(work, tarball)], app);

  const pkg = join(app, "node_modules/@rubics-code/agent-kit");
  for (const f of ["dist/index.js", "dist/index.d.ts", "dist/runtime/web/index.html", "dist/groups/cloudflare/guidance.md"]) {
    if (!existsSync(join(pkg, f))) throw new Error(`the package has no ${f}`);
  }
  if (existsSync(join(pkg, "src")) || existsSync(join(pkg, "test"))) throw new Error("the package holds src or test");

  writeFileSync(
    join(app, "check.mjs"),
    `
import { buildAgent, cloudflareGroup, loadCredentials, page } from "@rubics-code/agent-kit";
import { z } from "zod";
const policy = { agent: "Scout", operator: "Ada", maxKeyLimitUsd: 10,
  infisical: { readablePath: z.string(), writablePath: z.string(), envLabel: () => "prod" } };
const config = { name: "Scout", model: "test/model", groups: [cloudflareGroup], system: "You are Scout.", policy };
const creds = loadCredentials({ OPENROUTER_API_KEY: "x", CLOUDFLARE_API_TOKEN: "y" });
const { groups, tools } = buildAgent(config, creds, { deliver: async () => {} });
if (!groups[0].enabled || !tools.includes("cloudflare_create_dns_record")) throw new Error("the group did not mount");
if (!page("Scout").includes("<h1>Scout</h1>")) throw new Error("the page did not load");
console.log("pack check: ok,", tools.length, "tools");
`,
  );
  // zod comes with the kit as a dependency; the consumer imports it too.
  run("node", ["check.mjs"], app);

  // The types: a consumer file type-checks against the published .d.ts files,
  // with skipLibCheck off, so an error inside the package also fails.
  writeFileSync(
    join(app, "check.ts"),
    `import { type AgentConfig, type ToolGroupDef, cloudflareGroup, runAgent } from "@rubics-code/agent-kit";
const group: ToolGroupDef<{ ping(): Promise<string> }> = cloudflareGroup as never;
export const config: AgentConfig = { name: "Scout", model: "m", groups: [group, cloudflareGroup], system: "", policy: undefined as never };
export const start: (c: AgentConfig) => Promise<void> = runAgent;
`,
  );
  writeFileSync(
    join(app, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: {
        module: "nodenext", moduleResolution: "nodenext", target: "es2024", strict: true, noEmit: true,
        skipLibCheck: false, types: ["node"], typeRoots: [join(root, "node_modules/@types")],
      },
      files: ["check.ts"],
    }),
  );
  run(join(root, "node_modules/.bin/tsc"), ["-p", join(app, "tsconfig.json")], app);
  console.log("pack check: the types are ok");
} finally {
  rmSync(work, { recursive: true, force: true });
}
