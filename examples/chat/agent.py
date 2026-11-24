"""Run a chat model as a Yi player. Uses only the Python standard library."""

import argparse
import json
import os
import sys
from urllib.request import Request, urlopen


PROMPT = (
    "You are playing Go. Choose one legal move to maximize your final area score. "
    "Read the board before choosing. Save groups in atari, capture vulnerable "
    "stones, and protect territory. Pass when further play will not improve your score. "
    "Coordinates skip I. X is Black and O is White. Reply with a JSON object with a move field."
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True, help="server base URL ending in /v1")
    parser.add_argument("--model", required=True, help="model ID served by this endpoint")
    parser.add_argument("--token-env", help="optional environment variable holding the endpoint token")
    args = parser.parse_args()
    headers = {"Content-Type": "application/json"}
    if args.token_env:
        headers["Authorization"] = "Bearer " + os.environ[args.token_env]

    def api(path, body=None, timeout=5):
        data = None if body is None else json.dumps(body).encode()
        request = Request(args.base_url.rstrip("/") + path, data=data, headers=headers)
        with urlopen(request, timeout=timeout) as response:
            return json.load(response)

    for line in sys.stdin:
        event = json.loads(line)
        if event["type"] == "hello":
            if event["protocol"] != "yi-agent-v1":
                raise ValueError("unsupported protocol")
            if args.model not in {m["id"] for m in api("/models")["data"]}:
                raise ValueError("model ID is unavailable on the configured endpoint")
            reply = {"type": "ready", "protocol": event["protocol"], "id": event["id"]}
        elif event["type"] == "turn":
            observation = {k: event[k] for k in (
                "color", "size", "komi", "ascii", "history", "legal", "captures", "rules",
                "remainingMoves"
            )}
            schema = {
                "type": "object",
                "properties": {"move": {"type": "string", "enum": event["legal"]}},
                "required": ["move"], "additionalProperties": False,
            }
            result = api("/chat/completions", {
                "model": args.model, "temperature": 0, "max_tokens": 32,
                "messages": [
                    {"role": "system", "content": PROMPT},
                    {"role": "user", "content": json.dumps(observation, separators=(",", ":"))},
                ],
                "response_format": {"type": "json_schema", "json_schema": {
                    "name": "go_move", "strict": True, "schema": schema,
                }},
            }, timeout=event["timeLimitMS"] / 1000)
            move = json.loads(result["choices"][0]["message"]["content"])["move"]
            reply = {"type": "move", "id": event["id"], "revision": event["revision"], "move": move}
        else:
            break
        print(json.dumps(reply), flush=True)


if __name__ == "__main__":
    main()
