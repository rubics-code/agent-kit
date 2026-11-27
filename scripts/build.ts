// Compiles src/ to dist/ (JavaScript and .d.ts) and copies the text assets
// (guidance.md, index.html) that the code reads at runtime. Node does not strip
// types from files under node_modules, so the published package needs this.
import { execFileSync } from "node:child_process";
import { cpSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const src = join(root, "src");
const dist = join(root, "dist");

rmSync(dist, { recursive: true, force: true });
execFileSync(join(root, "node_modules/.bin/tsc"), ["-p", join(root, "tsconfig.build.json")], { stdio: "inherit" });

// tsc rewrites ./x.ts to ./x.js in JavaScript, not in .d.ts files.
for (const entry of readdirSync(dist, { recursive: true, withFileTypes: true })) {
  if (entry.isFile() && entry.name.endsWith(".d.ts")) {
    const file = join(entry.parentPath, entry.name);
    writeFileSync(file, readFileSync(file, "utf8").replace(/(from\s+["']\.{1,2}\/[^"']*?)\.ts(["'])/g, "$1.js$2"));
  }
}

for (const entry of readdirSync(src, { recursive: true, withFileTypes: true })) {
  if (entry.isFile() && /\.(md|html)$/.test(entry.name)) {
    const from = join(entry.parentPath, entry.name);
    cpSync(from, join(dist, from.slice(src.length)));
  }
}
