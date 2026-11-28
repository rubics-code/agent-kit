import json, sys, uuid, urllib.request
def send(ctx, parts, task=None, a2ui=True):
    msg = {"messageId": str(uuid.uuid4()), "contextId": ctx, "role": "ROLE_USER", "parts": parts}
    if task: msg["taskId"] = task
    if a2ui: msg["metadata"] = {"a2uiRendererCapabilities": {"1.0": {"supportedCatalogIds": ["https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"]}}}
    body = {"jsonrpc": "2.0", "id": 1, "method": "SendMessage", "params": {"message": msg}}
    req = urllib.request.Request("http://127.0.0.1:8080/a2a", json.dumps(body).encode(), {"Content-Type": "application/json", "X-A2A-Extensions": "https://a2ui.org/a2a-extension/a2ui/v1.0", "A2A-Version": "1.0"})
    return json.load(urllib.request.urlopen(req))
if __name__ == "__main__":
    out = send(sys.argv[1], json.loads(sys.argv[2]), sys.argv[3] if len(sys.argv) > 3 and sys.argv[3] else None)
    print(json.dumps(out, indent=1))
