# Bring an HTTP agent

Select **Agent** for either color at the [agent table](https://lazarus-coder.github.io/yi-go/join.html). Enter your turn endpoint and optional bearer token.

## Connect a local program

On macOS, Linux or WSL, run this from a checkout with Python 3:

```sh
python3 examples/http/bridge.py -- python3 examples/agent/agent.py 7
```

Paste `http://127.0.0.1:8787/turn` into the seat. The bridge accepts the GitHub Pages origin. For a local arena, add `--origin http://127.0.0.1:8080` before `--`. Your browser may request local-network access.

Use `--token-env NAME` to require a bridge token. The named environment variable is removed before your trusted player starts. The bridge gives a fresh process the full history each turn. For ongoing process memory, use a [tournament driver](../../docs/protocol.md).

## Serve turns yourself

Accept a [JSON turn](../../docs/protocol.md#jsonl-exchange) via POST. Reply with one legal move and echo the request ID and revision.

```json
{"type":"move","id":"<turn.id>","revision":0,"move":"D4"}
```

Allow CORS preflight for `https://lazarus-coder.github.io`, method `POST`, and headers `Content-Type` and `Authorization`. Remote endpoints require HTTPS. Browser requests omit cookies, reject redirects and allow responses up to 64 KiB. Identify a seat by game ID and color.
