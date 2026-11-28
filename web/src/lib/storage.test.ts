import { describe, expect, it } from "vitest";
import { agentId, loadAgents, loadThreads, saveAgents, saveThreads, upsertAgent, type Agent } from "./storage";
import { newThread } from "./thread";

class Mem implements Storage {
  private m = new Map<string, string>();
  get length() { return this.m.size; }
  clear() { this.m.clear(); }
  getItem(k: string) { return this.m.get(k) ?? null; }
  key(i: number) { return [...this.m.keys()][i] ?? null; }
  removeItem(k: string) { this.m.delete(k); }
  setItem(k: string, v: string) { this.m.set(k, v); }
}
const localStorage = new Mem();

const agent = (id: string, at = 1): Agent => ({ id, name: id, base: "http://x", endpoint: "/a2a", a2ui: true, connectedAt: at });

describe("storage", () => {
  it("keeps agents and keeps the first connect time", () => {
    let list = upsertAgent([], agent("a", 1));
    list = upsertAgent(list, { ...agent("a", 9), name: "renamed" });
    expect(list).toEqual([{ ...agent("a", 1), name: "renamed" }]);
    saveAgents(list, localStorage);
    expect(loadAgents(localStorage)).toEqual(list);
  });
  it("drops the streaming text when it saves a thread", () => {
    saveThreads([{ ...newThread("c", "a", 5), streaming: "half" }], localStorage);
    expect(loadThreads(localStorage)[0]).toMatchObject({ id: "c", streaming: undefined });
  });
  it("gives a bad store as empty", () => {
    localStorage.setItem("console.agents.v1", "{not json");
    expect(loadAgents(localStorage)).toEqual([]);
  });
  it("makes a stable agent id", () => {
    expect(agentId("http://Example.test:8080/")).toBe("example.test:8080");
  });
});
