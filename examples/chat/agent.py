"""Run a chat model as a Yi player. Uses only the Python standard library."""

import argparse
import json
import os
import sys
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


PROMPT = (
    "You are playing Go. Choose one legal move to maximize your final area score. "
    "Read the board before choosing. Save groups in atari, capture vulnerable "
    "stones, and protect territory. Pass when further play will not improve your score. "
    "Coordinates skip I. X is Black and O is White. Reply with a JSON object with a move field."
)


class NoRedirect(HTTPRedirectHandler):
    # A redirect must never carry a model credential to another endpoint.
    def redirect_request(self, *args):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=os.getenv("YI_CHAT_BASE_URL"), help="server API base URL (or YI_CHAT_BASE_URL)")
    parser.add_argument("--model", default=os.getenv("YI_CHAT_MODEL"), help="model ID (or YI_CHAT_MODEL)")
    parser.add_argument("--token-env", help="optional environment variable holding the endpoint token")
    parser.add_argument("--plain", action="store_true", help="use a plain chat completion without model discovery or constrained decoding")
    args = parser.parse_args()
    if not args.base_url or not args.model:
        parser.error("provide a base URL and model ID via flags or environment")
    url = urlsplit(args.base_url)
    if url.scheme not in ("https", "http") or not url.hostname or url.username is not None or url.password is not None or url.query or url.fragment:
        parser.error("use an HTTP(S) base URL without credentials, query or fragment")
    headers = {"Content-Type": "application/json"}
    if args.token_env:
        token = os.environ.get(args.token_env)
        if not token:
            parser.error("the configured token environment variable is empty")
        if url.scheme != "https" and url.hostname not in ("localhost", "127.0.0.1", "::1"):
            parser.error("a keyed endpoint needs HTTPS or loopback HTTP")
        headers["Authorization"] = "Bearer " + token
    client = build_opener(NoRedirect())

    def api(path, body=None, timeout=5):
        data = None if body is None else json.dumps(body).encode()
        request = Request(args.base_url.rstrip("/") + path, data=data, headers=headers)
        with client.open(request, timeout=timeout) as response:
            limit = 4 << 20 if path == "/models" else 64 << 10
            data = response.read(limit + 1)
            if len(data) > limit:
                raise ValueError("model response exceeds the size limit")
            return json.loads(data)

    for line in sys.stdin:
        event = json.loads(line)
        if event["type"] == "hello":
            if event["protocol"] != "yi-agent-v1":
                raise ValueError("unsupported protocol")
            if not args.plain and args.model not in {m["id"] for m in api("/models")["data"]}:
                raise ValueError("model ID is unavailable on the configured endpoint")
            reply = {"type": "ready", "protocol": event["protocol"], "id": event["id"]}
        elif event["type"] == "turn":
            observation = {k: event[k] for k in (
                "color", "size", "komi", "ascii", "history", "legal", "captures", "rules",
                "remainingMoves"
            )}
            payload = {"model": args.model, "messages": [
                {"role": "system", "content": PROMPT},
                {"role": "user", "content": json.dumps(observation, separators=(",", ":"))},
            ]}
            if args.plain:
                cap = "max_completion_tokens" if url.hostname == "api.openai.com" else "max_tokens"
                payload[cap] = 256
            else:
                schema = {
                    "type": "object",
                    "properties": {"move": {"type": "string", "enum": event["legal"]}},
                    "required": ["move"], "additionalProperties": False,
                }
                payload.update(temperature=0, max_tokens=32)
                payload["response_format"] = {"type": "json_schema", "json_schema": {
                    "name": "go_move", "strict": True, "schema": schema,
                }}
            result = api("/chat/completions", payload, timeout=event["timeLimitMS"] / 1000)
            content = result["choices"][0]["message"]["content"].strip()
            move = content if args.plain and content in event["legal"] else json.loads(content)["move"]
            if move not in event["legal"]:
                raise ValueError("model returned an illegal move")
            reply = {"type": "move", "id": event["id"], "revision": event["revision"], "move": move}
        else:
            break
        print(json.dumps(reply), flush=True)


if __name__ == "__main__":
    main()
