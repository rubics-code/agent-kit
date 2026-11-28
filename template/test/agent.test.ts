// {{Name}}'s own wiring. The Kit's tests cover the machinery.
import { describe, expect, it } from "vitest";
import { buildAgent, loadCredentials, page } from "@rubics-code/agent-kit";
import { AGENT, POLICY } from "../src/agent.ts";
import { memoryNotes, notesGroup } from "../src/groups/notes.ts";

describe("{{Name}}", () => {
  it("has a name that the web page accepts", () => {
    expect(page(AGENT.name)).toContain("<h1>{{Name}}</h1>");
  });

  it("builds with the notes group on", () => {
    const { groups, tools } = buildAgent(AGENT, loadCredentials({ OPENROUTER_API_KEY: "x" }), { deliver: async () => {} });
    expect(groups.find((g) => g.id === "notes")?.enabled).toBe(true);
    expect(tools).toEqual(expect.arrayContaining(["notes_list", "notes_add"]));
  });

  it("lists the notes", async () => {
    const [list] = notesGroup.tools(memoryNotes(["call the bank"]), { secrets: { deliver: async () => {} }, policy: POLICY });
    expect(await list!.invoke({})).toContain("call the bank");
  });
});
