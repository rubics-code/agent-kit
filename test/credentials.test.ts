import { describe, expect, it } from "vitest";
import { loadCredentials } from "./fixture.ts";

const vault = {
  INFISICAL_AGENT_VAULT_SESSION_TOKEN: "session",
  HTTPS_PROXY: "http://localhost:17323",
  NODE_USE_ENV_PROXY: "1",
};

describe("loadCredentials", () => {
  it("reads the real values from the environment in env mode", () => {
    const c = loadCredentials({ MXROUTE_API_KEY: "real" });

    expect(c.mode).toBe("env");
    expect(c.get("MXROUTE_API_KEY")).toBe("real");
    expect(c.optional("OPENROUTER_MANAGEMENT_KEY")).toBeUndefined();
    expect(() => c.get("OPENROUTER_API_KEY")).toThrow("OPENROUTER_API_KEY is not set");
  });

  it("gives placeholders, not keys, in Agent Vault mode", () => {
    const c = loadCredentials(vault);

    expect(c.mode).toBe("agent-vault");
    expect(c.get("MXROUTE_API_KEY")).toBe("__MXROUTE_API_KEY__");
    expect(c.optional("OPENROUTER_MANAGEMENT_KEY")).toBe("__OPENROUTER_MANAGEMENT_KEY__");
  });

  it("stops when Node would not send the requests through the proxy", () => {
    expect(() => loadCredentials({ ...vault, NODE_USE_ENV_PROXY: undefined })).toThrow("NODE_USE_ENV_PROXY=1");
    expect(() => loadCredentials({ ...vault, HTTPS_PROXY: undefined })).toThrow("HTTPS_PROXY is not set");
  });

  it("stops when a real key is also in the environment in Agent Vault mode", () => {
    const c = loadCredentials({ ...vault, MXROUTE_API_KEY: "real" });

    expect(() => c.get("MXROUTE_API_KEY")).toThrow("MXROUTE_API_KEY is in the environment");
  });

  it("sees Agent Vault mode from the proxy user alone, as agent-vault run 0.45.4 sets it", () => {
    const c = loadCredentials({ HTTPS_PROXY: "http://x-agent-vault:tok@100.64.0.1:17323", NODE_USE_ENV_PROXY: "1" });
    expect(c.mode).toBe("agent-vault");
    expect(c.get("MXROUTE_API_KEY")).toBe("__MXROUTE_API_KEY__");
  });

  it("stays in env mode behind another proxy", () => {
    expect(loadCredentials({ HTTPS_PROXY: "http://corp:pw@proxy:8080", MXROUTE_API_KEY: "real" }).mode).toBe("env");
  });
});
