import { describe, expect, it } from "vitest";
import { MemoryInfisical, deliverSecret, inboxSink, noSink, startHealth } from "../src/index.ts";
import { rubyPolicy } from "./fixture.ts";

describe("container mode", () => {
  it("puts a new secret in the inbox folder with the label and the time", async () => {
    const store = new MemoryInfisical();
    const sink = inboxSink(store, "/ruby/inbox", () => new Date("2026-11-28T09:30:12.345Z"));
    const reply = await deliverSecret({ policy: rubyPolicy, secrets: sink }, "mailbox greg@rubicscode.com", "pw");
    expect(await store.listSecretNames("/ruby/inbox")).toEqual(["MAILBOX_GREG_RUBICSCODE_COM_20261128_093012"]);
    expect(reply).toBe("It went directly to the Infisical inbox folder /ruby/inbox. You do not have it.");
    expect(reply).not.toContain("pw");
  });

  it("gives a name that Infisical accepts for any label", async () => {
    const store = new MemoryInfisical();
    await inboxSink(store, "/ruby/inbox", () => new Date("2026-11-28T09:30:12Z")).deliver("1 key: ünïcode!", "v");
    const [name] = await store.listSecretNames("/ruby/inbox");
    expect(name).toMatch(/^[A-Z][A-Z0-9_]{0,127}$/);
  });

  it("refuses a secret with no place to go", async () => {
    await expect(noSink.deliver("key", "v")).rejects.toThrow("Call again with storeAt");
  });

  it("answers the health check on all addresses", async () => {
    const h = await startHealth(0, "127.0.0.1");
    try {
      const ok = await fetch(`http://127.0.0.1:${h.port}/healthz`);
      expect(ok.status).toBe(200);
      expect(await ok.json()).toEqual({ status: "ok" });
      expect((await fetch(`http://127.0.0.1:${h.port}/api/thread`)).status).toBe(404);
    } finally {
      h.server.close();
    }
  });
});
