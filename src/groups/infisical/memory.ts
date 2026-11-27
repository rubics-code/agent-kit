import type { SecretPlace, SecretStore } from "../../secret-store.ts";

// In-memory Infisical for tests and evals. Folder path -> secret name -> value.
export class MemoryInfisical implements SecretStore {
  readonly folders = new Map<string, Map<string, string>>([["/", new Map()]]);
  readonly writes: { op: string; path: string; name?: string }[] = [];

  constructor(seed: Record<string, Record<string, string>> = {}) {
    for (const [path, secrets] of Object.entries(seed)) {
      this.#mkdirs(path);
      for (const [k, v] of Object.entries(secrets)) this.folders.get(path)!.set(k, v);
    }
  }

  #mkdirs(path: string, record = false) {
    let at = "";
    for (const part of path.split("/").filter(Boolean)) {
      at = `${at}/${part}`;
      if (!this.folders.has(at)) {
        this.folders.set(at, new Map());
        if (record) this.writes.push({ op: "createFolder", path: at });
      }
    }
  }

  async listFolders(path: string) {
    const prefix = path === "/" ? "/" : `${path}/`;
    return [...this.folders.keys()]
      .filter((p) => p.startsWith(prefix) && p !== path && !p.slice(prefix.length).includes("/"))
      .map((p) => p.slice(prefix.length))
      .sort();
  }

  async listSecretNames(path: string) {
    return [...(this.folders.get(path)?.keys() ?? [])].sort();
  }

  async check(place: SecretPlace) {
    const folder = this.folders.get(place.path);
    return { folderExists: !!folder, exists: !!folder?.has(place.name) };
  }

  async put(place: SecretPlace, value: string) {
    this.#mkdirs(place.path, true);
    const folder = this.folders.get(place.path)!;
    this.writes.push({ op: folder.has(place.name) ? "updateSecret" : "createSecret", path: place.path, name: place.name });
    folder.set(place.name, value);
  }
}
