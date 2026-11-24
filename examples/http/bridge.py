#!/usr/bin/env python3
"""Expose a trusted, history-based Yi JSONL player to the browser on loopback."""
import argparse
import hmac
import json
import os
import queue
import re
import select
import signal
import socket
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit

LIMIT = 64 * 1024
PROTOCOL = "yi-agent-v1"
PAGES_ORIGIN = "https://lazarus-coder.github.io"


def exchange(command, env, turn, connection):
    hello = {k: turn[k] for k in ("gameId", "color", "size", "komi", "players",
                                  "maxMoves", "turnTimeoutMS", "revision", "remainingMoves")}
    hello.update(type="hello", protocol=PROTOCOL, id=turn["id"] + ":hello")
    ready = {"type": "ready", "protocol": PROTOCOL, "id": hello["id"]}
    deadline = time.monotonic() + min(30, turn["timeLimitMS"] / 1000)
    proc = subprocess.Popen(command, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, start_new_session=True)
    result = queue.Queue(maxsize=1)

    def ask(event):
        proc.stdin.write(json.dumps(event, separators=(",", ":")).encode() + b"\n")
        proc.stdin.flush()
        line = proc.stdout.readline(LIMIT + 1)
        if not line.endswith(b"\n") or len(line) > LIMIT:
            raise ValueError("invalid JSONL response")
        return json.loads(line)

    def play():
        try:
            if ask(hello) != ready:
                raise ValueError("invalid readiness response")
            reply = ask(turn)
            if (not isinstance(reply, dict) or set(reply) != {"type", "id", "revision", "move"}
                    or reply["type"] != "move" or reply["id"] != turn["id"]
                    or type(reply["revision"]) is not int or reply["revision"] != turn["revision"]
                    or not isinstance(reply["move"], str)):
                raise ValueError("invalid move response")
            result.put(reply)
        except (ValueError, OSError, RecursionError):
            result.put(None)

    worker = threading.Thread(target=play, daemon=True)
    try:
        worker.start()
        while time.monotonic() < deadline:
            # Browsers close or reset this HTTP/1 connection when Stop is used.
            if select.select([connection], [], [], 0)[0] and connection.recv(1, socket.MSG_PEEK) == b"":
                raise ConnectionAbortedError
            try:
                reply = result.get(timeout=min(0.05, max(0, deadline - time.monotonic())))
            except queue.Empty:
                continue
            if reply is None:
                raise ValueError("agent failed")
            return reply
        raise TimeoutError
    finally:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        proc.wait()
        worker.join()
        proc.stdin.close()
        proc.stdout.close()


def valid_turn(event):
    if not isinstance(event, dict) or event.get("type") != "turn" or event.get("protocol") != PROTOCOL:
        return False
    if event.get("size") != 9 or event.get("komi") != 6.5 or event.get("color") not in ("black", "white"):
        return False
    if not all(isinstance(event.get(k), str) and 0 < len(event[k]) <= 256 for k in ("id", "gameId")):
        return False
    budgets = ("timeLimitMS", "turnTimeoutMS", "maxMoves", "remainingMoves", "revision")
    if not all(type(event.get(k)) is int for k in budgets):
        return False
    revision = event["revision"]
    if not (1 <= event["timeLimitMS"] <= event["turnTimeoutMS"] <= 120000
            and 0 <= revision < event["maxMoves"] <= 144 and event["remainingMoves"] == event["maxMoves"] - revision):
        return False
    board, history, legal = event.get("board"), event.get("history"), event.get("legal")
    return (isinstance(board, list) and len(board) == 81 and all(type(cell) is int and 0 <= cell <= 2 for cell in board)
            and isinstance(event.get("ascii"), str) and isinstance(event.get("rules"), dict)
            and isinstance(event.get("captures"), dict) and isinstance(event.get("players"), dict)
            and all(isinstance(event["players"].get(k), str) and type(event["captures"].get(k)) is int
                    for k in ("black", "white"))
            and isinstance(history, list) and len(history) == revision
            and isinstance(legal, list) and 0 < len(legal) <= 83
            and all(isinstance(m, str) and re.fullmatch(r"[A-HJ][1-9]|pass|resign", m) for m in history + legal))


class Turns(BaseHTTPRequestHandler):
    timeout = 5  # Bound time waiting for incoming request data.

    def log_message(self, *args):
        pass

    def respond(self, status, body=None):
        data = json.dumps(body).encode() if body is not None else b""
        self.send_response(status)
        if self.headers.get("Origin") == self.server.origin:
            self.send_header("Access-Control-Allow-Origin", self.server.origin)
            self.send_header("Vary", "Origin")
            if self.command == "OPTIONS":
                self.send_header("Access-Control-Allow-Methods", "POST")
                self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
                self.send_header("Access-Control-Allow-Private-Network", "true")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if data:
            self.wfile.write(data)

    def check_origin_and_path(self):
        if self.path != "/turn":
            self.respond(404)
        elif self.headers.get("Origin") != self.server.origin:
            self.respond(403)
        else:
            return True
        return False

    def do_OPTIONS(self):
        if self.check_origin_and_path():
            self.respond(204)

    def do_POST(self):
        if not self.check_origin_and_path():
            return
        token = self.server.token
        if token and not hmac.compare_digest(self.headers.get("Authorization", "").encode(), b"Bearer " + token):
            return self.respond(401)
        if self.headers.get_content_type() != "application/json" or "Transfer-Encoding" in self.headers:
            return self.respond(415)
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= LIMIT:
                return self.respond(413)
            event = json.loads(self.rfile.read(length))
            if not valid_turn(event):
                return self.respond(400)
        except (ValueError, RecursionError, socket.timeout):
            return self.respond(400)
        try:
            reply = exchange(self.server.command, self.server.env, event, self.connection)
        except TimeoutError:
            return self.respond(504)
        except (ConnectionAbortedError, BrokenPipeError, ConnectionResetError):
            return
        except (OSError, ValueError):
            return self.respond(502)
        self.respond(200, reply)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8787)
    parser.add_argument("--origin", default=PAGES_ORIGIN, help="one exact browser origin allowed")
    parser.add_argument("--token-env", help="name of an environment variable holding the bridge token")
    parser.add_argument("command", nargs=argparse.REMAINDER, help="-- followed by a trusted JSONL program and arguments")
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or os.name != "posix":
        parser.error("supply a player command on macOS, Linux or WSL")
    origin = urlsplit(args.origin)
    if (origin.scheme not in ("http", "https") or not origin.hostname or origin.username is not None
            or args.origin != origin.scheme + "://" + origin.netloc):
        parser.error("supply one exact HTTP(S) origin without a path")
    env = os.environ.copy()
    token = env.pop(args.token_env, "") if args.token_env else ""
    if args.token_env and (not token or not token.isascii() or not token.isprintable() or " " in token):
        parser.error("set the token environment variable to a printable bearer token")
    def stop(signum, frame):
        raise SystemExit

    signal.signal(signal.SIGTERM, stop)
    with HTTPServer(("127.0.0.1", args.port), Turns) as server:
        server.origin, server.command, server.env, server.token = args.origin, command, env, token.encode()
        print(f"Agent endpoint http://127.0.0.1:{server.server_port}/turn", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
