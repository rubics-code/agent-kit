// Container mode: no terminal. The web chat stays on 127.0.0.1 inside the pod
// (reach it with kubectl port-forward), and a health server on all addresses
// answers the kubelet.
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import type { SecretStore } from "../secret-store.ts";
import type { SecretSink } from "../secrets.ts";

// A new secret with no storeAt goes to an Infisical inbox folder, because no
// terminal is there to show it. The name holds the label and the time, so a
// second secret never replaces the first.
export function inboxSink(store: SecretStore, path: string, now: () => Date = () => new Date()): SecretSink {
  return {
    where: `the Infisical inbox folder ${path}`,
    async deliver(label, value) {
      const stamp = now().toISOString().replace(/[-:]/g, "").replace(/\..*$/, "").replace("T", "_");
      const base = label.toUpperCase().replace(/[^A-Z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "SECRET";
      const head = /^[A-Z]/.test(base) ? base : `S_${base}`;
      const name = `${head.slice(0, 110)}_${stamp}`;
      await store.put({ path, name }, value);
    },
  };
}

// A sink for a container with no secret store. The tool reports the error.
export const noSink: SecretSink = {
  async deliver(label) {
    throw new Error(
      `no place for the new secret for ${label}: the agent runs in a container without Infisical. Call again with storeAt.`,
    );
  },
};

export async function startHealth(port: number, host = "0.0.0.0") {
  const server = createServer((req, res) => {
    const path = new URL(req.url ?? "/", "http://x").pathname;
    if (req.method === "GET" && path === "/healthz") {
      res.writeHead(200, { "Content-Type": "application/json", "Cache-Control": "no-store" });
      res.end('{"status":"ok"}');
      return;
    }
    res.writeHead(404).end();
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => resolve());
  });
  return { server, port: (server.address() as AddressInfo).port };
}
