import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// In dev, the page talks to a local agent through this proxy, so it needs no CORS.
const agent = process.env.AGENT_URL ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/a2a": agent, "/.well-known": agent, "/kit": agent } },
  test: { environment: "jsdom", include: ["src/**/*.test.ts", "src/**/*.test.tsx"] },
});
