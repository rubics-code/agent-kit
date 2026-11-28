import { describe, expect, it } from "vitest";
import type { StreamEvent } from "./a2a";
import { addUser, applyEvent, decide, newThread, titleFrom, type Thread } from "./thread";
import hello from "./fixtures/hello.json";
import write from "./fixtures/write.json";
import approve from "./fixtures/approve.json";

const run = (t: Thread, evs: unknown[]) => (evs as StreamEvent[]).reduce((acc, e, i) => applyEvent(acc, e, 1000 + i), t);

describe("applyEvent", () => {
  it("streams text, then keeps the final reply once", () => {
    let t = addUser(newThread("c1", "a", 0), "say hello please", 0);
    const mid = run(t, hello.slice(0, 5));
    expect(mid.streaming).toBe("Hello. I am ");
    t = run(t, hello);
    expect(t.streaming).toBeUndefined();
    expect(t.state).toBe("TASK_STATE_COMPLETED");
    const agent = t.items.filter((i) => i.kind === "agent");
    expect(agent).toHaveLength(1);
    expect(agent[0]).toMatchObject({ text: "Hello. I am the dev agent, and I run on the fake model." });
  });

  it("makes an approval item with the summary that code wrote", () => {
    const t = run(addUser(newThread("c2", "a", 0), "Write a note", 0), write);
    expect(t.state).toBe("TASK_STATE_INPUT_REQUIRED");
    expect(t.pendingTaskId).toBeTruthy();
    const tool = t.items.find((i) => i.kind === "tool");
    expect(tool).toMatchObject({ name: "write_note", state: "waiting" });
    const ap = t.items.find((i) => i.kind === "approval");
    expect(ap).toMatchObject({ approval: { tool: "write_note", summary: 'Write the note "Groceries" (10 characters): "milk, eggs"' } });
    expect(ap && ap.kind === "approval" && ap.surfaceId).toBe(`approval-${ap && ap.kind === "approval" ? ap.confirmationId : ""}`);
    expect(ap && ap.kind === "approval" && ap.messages.length).toBe(2);
  });

  it("finishes after the approval", () => {
    let t = run(newThread("c2", "a", 0), write);
    const ap = t.items.find((i) => i.kind === "approval")!;
    t = decide(t, ap.kind === "approval" ? ap.confirmationId : "", "approved", 2000);
    t = run(t, approve);
    expect(t.pendingTaskId).toBeUndefined();
    expect(t.items.find((i) => i.kind === "approval")).toMatchObject({ decision: "approved" });
    expect(t.items.at(-1)).toMatchObject({ kind: "agent", text: 'The note "Groceries" is written.' });
    expect(t.items.filter((i) => i.kind === "tool")).toMatchObject([{ name: "write_note", state: "done" }]);
  });

  it("shows an error event", () => {
    const t = applyEvent(newThread("c", "a", 0), { error: "boom" }, 1);
    expect(t.items[0]).toMatchObject({ kind: "error", text: "boom" });
  });
});

describe("titleFrom", () => {
  it("cuts a long first message", () => {
    expect(titleFrom("a ".repeat(60)).length).toBe(48);
    expect(titleFrom("  ")).toBe("New thread");
  });
});
