import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

// In dev, the page talks to a local agent through this proxy, so it needs no CORS.
const agent = process.env.AGENT_URL ?? "http://127.0.0.1:8080";

export default defineConfig({
  // dist/.keep stays in git, so the Go embed has a file before the first build.
  plugins: [react(), { name: "keep-dist", closeBundle: () => writeFileSync(resolve(import.meta.dirname, "dist/.keep"), "") }],
  server: { proxy: { "/a2a": agent, "/.well-known": agent, "/kit": agent } },
  test: { environment: "jsdom", include: ["src/**/*.test.ts", "src/**/*.test.tsx"] },
});
