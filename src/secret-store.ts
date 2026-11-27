// What an agent's tools can do with a secret store. No call returns a secret
// value. The Infisical HTTP client is one adapter; tests use in-memory ones.
export interface SecretPlace {
  path: string; // folder, for example /agents/lyra
  name: string; // secret key, for example OPENROUTER_API_KEY
}

export interface SecretStore {
  listFolders(path: string): Promise<string[]>;
  listSecretNames(path: string): Promise<string[]>;
  // Whether the folder and the secret exist now.
  check(place: SecretPlace): Promise<{ folderExists: boolean; exists: boolean }>;
  // Makes missing folders, then creates the secret or replaces its value.
  put(place: SecretPlace, value: string): Promise<void>;
}
