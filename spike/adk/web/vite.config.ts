import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The page talks only to the agent's A2A endpoint. The proxy avoids CORS.
const agent = process.env.AGENT_URL ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/a2a": agent, "/.well-known": agent } },
});
