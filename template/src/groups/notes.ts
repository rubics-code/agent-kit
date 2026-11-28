// An example tool group: notes in home/memory/notes.md. It shows the parts
// of a group: a service, a read tool, and a write tool behind the approval
// gate. Replace it with the agent's own groups.
import { appendFile, mkdir, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { approvedTool, result, type ToolGroupDef } from "@rubics-code/agent-kit";
import { tool } from "langchain";
import { z } from "zod";

// The service. The tools know only this interface, so a test or an eval can
// give an in-memory one.
export interface Notes {
  list(): Promise<string[]>;
  add(text: string): Promise<void>;
}

export function fileNotes(file: string): Notes {
  return {
    async list() {
      try {
        return (await readFile(file, "utf8")).split("\n").filter(Boolean);
      } catch (err) {
        if ((err as NodeJS.ErrnoException).code === "ENOENT") return [];
        throw err;
      }
    },
    async add(text) {
      await mkdir(dirname(file), { recursive: true });
      await appendFile(file, `${text.replace(/\s+/g, " ").trim()}\n`);
    },
  };
}

export function memoryNotes(items: string[] = []): Notes & { items: string[] } {
  return {
    items,
    async list() {
      return [...items];
    },
    async add(text) {
      items.push(text);
    },
  };
}

const NOTES_FILE = fileURLToPath(new URL("../../home/memory/notes.md", import.meta.url));

export const notesGroup: ToolGroupDef<Notes> = {
  id: "notes",
  name: "Notes",
  // A group that calls an API gives its host (for the Agent Vault session
  // check) and its env keys. This group has neither.
  host: "",
  keys: [],
  setup: "",
  guidance: "Notes: list the notes before you add one, so that you do not add the same note two times.",
  // No keys and no host, so the group is on in each key mode.
  review: () => ({ enabled: true, missing: [] }),
  // Makes the service from the session's keys. Return undefined when a key is missing.
  connect: () => fileNotes(NOTES_FILE),
  tools(notes, shared) {
    const list = tool(() => result(() => notes.list()), {
      name: "notes_list",
      description: "List the notes.",
      schema: z.object({}),
    });

    // A write tool. The operator sees the summary, which code writes from the
    // arguments, and must approve it before run.
    const add = approvedTool({
      name: "notes_add",
      description: "Add one note.",
      operator: shared.policy.operator,
      schema: z.object({ text: z.string().min(1).max(500).describe("The note text") }),
      prepare: async ({ text }) => ({ summary: `Add the note "${text}"` }),
      run: async ({ text }) => {
        await notes.add(text);
        return "Added.";
      },
    });

    return [list, add];
  },
};
