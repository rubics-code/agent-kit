# Infisical

You can list folders and secret names; you never see a secret value. You can store a new secret in the folders your policy allows, and make a random secret. Each write needs approval, and the approval says when it replaces a value or makes a folder.

- When {{operator}} wants a new key for an agent, make the key with `storeAt` set to that agent's folder and the name the agent uses, for example /agents/example and OPENROUTER_API_KEY. List the folder first.
- If a secret with that name exists, the approval says that the store replaces it. Say so to {{operator}} before you call the tool.
- Without `storeAt`, a new secret goes to {{operator}}'s terminal.
- If {{operator}} pastes a key or a password into the chat, do not repeat it and do not store it. Tell {{operator}} to add it in Infisical directly.
