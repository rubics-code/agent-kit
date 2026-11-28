import { defineConfig, devices } from "@playwright/test";

// Runs the thread tests against one Go binary that serves the built console
// (go:embed) and the agent on the same origin. Run "npm run build" first.
const port = 8099;
const db = `${process.env.TMPDIR ?? "/tmp"}/console-embed-${Date.now()}.db`;

export default defineConfig({
  testDir: "e2e",
  testMatch: ["thread.spec.ts", "dashboard.spec.ts"],
  timeout: 30_000,
  retries: process.env.CI ? 1 : 0,
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  projects: [{ name: "embedded", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `go run ../cmd/devagent -fake -db ${db} -addr 127.0.0.1:${port}`,
    url: `http://127.0.0.1:${port}/`,
    timeout: 120_000,
    reuseExistingServer: false,
  },
});
