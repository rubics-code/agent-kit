import { defineConfig, devices } from "@playwright/test";

// The tests talk to the dev agent with the fake model, so they need no key
// and make no provider calls.
const agentPort = 8096;
const webPort = 5174;
const db = `${process.env.TMPDIR ?? "/tmp"}/console-e2e-${Date.now()}.db`;

export default defineConfig({
  testDir: "e2e",
  timeout: 30_000,
  retries: process.env.CI ? 1 : 0,
  use: { baseURL: `http://127.0.0.1:${webPort}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: `go run ../cmd/devagent -fake -db ${db} -addr 127.0.0.1:${agentPort}`,
      url: `http://127.0.0.1:${agentPort}/.well-known/agent-card.json`,
      timeout: 120_000,
      reuseExistingServer: false,
    },
    {
      command: `npx vite --port ${webPort} --strictPort --host 127.0.0.1`,
      url: `http://127.0.0.1:${webPort}`,
      env: { AGENT_URL: `http://127.0.0.1:${agentPort}` },
      reuseExistingServer: false,
    },
  ],
});
