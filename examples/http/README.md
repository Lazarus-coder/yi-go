# Bring an HTTP agent

Choose **自有 Agent · HTTP** for either seat in the [open exhibition](https://lazarus-coder.github.io/yi-go/join.html). Enter the full URL of a service you control. Your browser sends one JSON request when that seat has a turn. You can also choose a chat-completions provider for either seat and enter its model ID, base URL and optional bearer key.

## Try a JSON-lines program

From a checkout, start this bridge on macOS, Linux or WSL with Python 3:

```sh
python3 examples/http/bridge.py -- python3 examples/agent/agent.py 7
```

Enter `http://127.0.0.1:8787/turn` on the exhibition page. The bridge listens on IPv4 loopback and accepts the GitHub Pages origin. If the browser asks for permission to connect to the local network, grant it for this exhibition. To run the exhibition from your own Yi server on port 8080, set its exact origin:

```sh
python3 examples/http/bridge.py --origin http://127.0.0.1:8080 -- python3 examples/agent/agent.py 7
```

Use `--port` to change 8787. To require a bearer token, set a private environment variable and pass its **name** with `--token-env YI_AGENT_TOKEN`. Enter the value under the same seat’s token field. The bridge removes that variable from the child environment.

The command after `--` is a program you trust to run under your account. Each turn starts a fresh process, sends the usual JSONL `hello` and complete `turn`, reads the move, and cleans up the process group. The server handles one request at a time, applying a 30-second ceiling. Use the tournament command or OCI driver for agents that need to keep an in-memory search tree between moves. A custom HTTP service can keep its own memory per game and color. [Tournament drivers and JSONL protocol](../../docs/protocol.md)

## Implement the endpoint directly

Accept `POST` with a JSON [turn observation](../../docs/protocol.md#jsonl-exchange). The browser supplies the full history, board, side, move budget, referee’s legal moves, and the same public game ID for both seats. The HTTP turn is self-contained. Reply directly with JSON whose request ID and revision match the observation:

```json
{"type":"move","id":"<copy turn.id>","revision":0,"move":"D4"}
```

A supplied token arrives as `Authorization: Bearer …`. Enable CORS preflight (`OPTIONS`) for the exhibition’s **origin**, `POST`, `Content-Type` and `Authorization`. For the published site the origin is `https://lazarus-coder.github.io`. Use HTTPS for remote endpoints. HTTP to `127.0.0.1` or `localhost` works on browsers that allow local-network access. The browser refuses redirects, omits cookies and limits responses to 64 KiB.

## Bring a model key

For a chat seat, enter a base URL such as `https://api.openai.com/v1` or a compatible local inference server’s URL. Yi appends `/chat/completions`. The endpoint must support browser CORS and return the selected move in `choices[0].message.content` as a coordinate, `pass`, `resign`, or a JSON object with a `move` field. Each turn uses one request with a 256-output-token limit requested from the provider. The direct OpenAI endpoint receives `max_completion_tokens`; other endpoints receive `max_tokens`. The browser never retries a rejected or interrupted request. A provider may bill for work already started when you press Stop. [Chat Completions API](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)

To use a provider that disallows browser requests, run your own agent service or enter its model through the trusted command driver using the [local BYOK configuration](../byok.json) and [Python chat adapter](../chat/agent.py). The chat adapter takes its provider key from the environment variable named with `--token-env`.
