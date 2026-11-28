import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { displayName, envPrefix, initAgent } from "../src/cli/init.ts";

const dirs: string[] = [];
const tmp = () => {
  const d = mkdtempSync(join(tmpdir(), "agent-kit-init-"));
  dirs.push(d);
  return d;
};
afterEach(() => {
  for (const d of dirs.splice(0)) rmSync(d, { recursive: true, force: true });
});

describe("initAgent", () => {
  it("makes the agent folder with every placeholder filled", () => {
    const dir = join(tmp(), "ops-bot");
    const files = initAgent({ name: "ops-bot", dir, operator: "Ada" });
    expect(files).toEqual(
      expect.arrayContaining([
        ".gitignore",
        ".dockerignore",
        "Dockerfile",
        "entrypoint.sh",
        "package.json",
        "src/agent.ts",
        "src/main.ts",
        "src/groups/notes.ts",
        "home/prompts/system.md",
        "home/memory/README.md",
        "test/agent.test.ts",
        "evals/notes.eval.ts",
      ]),
    );
    for (const f of files) expect(readFileSync(join(dir, f), "utf8"), f).not.toMatch(/\{\{\w+\}\}/);
    expect(readFileSync(join(dir, "src/agent.ts"), "utf8")).toContain('name: "Ops Bot"');
    expect(readFileSync(join(dir, "home/prompts/system.md"), "utf8")).toContain("You work for Ada.");
    expect(readFileSync(join(dir, "Dockerfile"), "utf8")).toContain("ENV OPS_BOT_HEADLESS=1");
  });

  it("writes a package.json with the kit and its peer packages", () => {
    const dir = join(tmp(), "scout");
    initAgent({ name: "scout", dir });
    const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
    expect(pkg.name).toBe("scout");
    expect(pkg.scripts.chat).toBe("node src/main.ts");
    expect(Object.keys(pkg.dependencies).sort()).toEqual(["@rubics-code/agent-kit", "langchain", "zod"]);
    expect(Object.keys(pkg.devDependencies).sort()).toEqual(["@types/node", "typescript", "vitest"]);
    expect(readFileSync(join(dir, "home/prompts/system.md"), "utf8")).toContain("You work for the operator.");
  });

  it("refuses a bad name and a folder that exists", () => {
    expect(() => initAgent({ name: "Scout", dir: join(tmp(), "x") })).toThrow(/not a valid agent name/);
    expect(() => initAgent({ name: "scout", dir: tmp() })).toThrow(/exists/);
  });

  it("names the agent like runAgent does", () => {
    expect(displayName("ops-bot")).toBe("Ops Bot");
    expect(envPrefix("Ops Bot")).toBe("OPS_BOT");
  });
});
