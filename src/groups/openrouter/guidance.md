# OpenRouter keys

A management key manages the other OpenRouter API keys; it cannot call a model. You can list the keys and read the credits, and you can create a key, change its spend limit, and disable or enable it. A disabled key stops at once.

Every key you make has a spend limit of at most {{maxKeyLimitUsd}} USD. You cannot make a key with no limit or remove a limit. If {{operator}} does not give a limit, ask for one.

You never see a key value: a new key goes to {{operator}} directly, or into Infisical with `storeAt`.
