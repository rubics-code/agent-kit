import { defineConfig } from "vitest/config";

// template/ holds an agent's own tests, with placeholders; they run only in a made agent.
export default defineConfig({ test: { include: ["test/**/*.test.ts"] } });
