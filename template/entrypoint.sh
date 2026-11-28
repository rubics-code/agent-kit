#!/bin/sh
# Container entrypoint. With INFISICAL_AGENT_VAULT_PROXY_ADDRESS set, {{Name}}
# runs behind Agent Vault: the CLI mints a session with the machine identity
# (INFISICAL_UNIVERSAL_AUTH_CLIENT_ID and _SECRET), and {{Name}} never gets the
# identity or a key. Without it, {{Name}} reads its keys from the environment.
set -eu
cd /app
if [ -n "${INFISICAL_AGENT_VAULT_PROXY_ADDRESS:-}" ]; then
  set -- --no-skills --label "${AGENT_VAULT_LABEL:-{{name}}}"
  if [ -n "${AGENT_VAULT_CA_FINGERPRINT:-}" ]; then set -- "$@" --ca-fingerprint "$AGENT_VAULT_CA_FINGERPRINT"; fi
  exec infisical agent-vault run "$@" -- \
    env -u INFISICAL_UNIVERSAL_AUTH_CLIENT_ID -u INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET node src/main.ts
fi
exec node src/main.ts
