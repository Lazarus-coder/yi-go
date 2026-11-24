#!/usr/bin/env python3
"""Bridge yi-agent-v1 JSONL to a persistent GTP v2 engine. Standard library only."""
import argparse
import json
import math
import queue
import re
import subprocess
import sys
import threading
import time

MAX_LINE = 16 * 1024
MAX_MESSAGE = 64 * 1024


class EngineError(RuntimeError):
    pass


def vertex(value, resign=False):
    value = value.strip().upper()
    if value == "PASS" or (resign and value == "RESIGN"):
        return value.lower()
    if not re.fullmatch(r"[A-HJ-T](?:[1-9]|1[0-9])", value):
        raise EngineError("invalid GTP vertex")
    return value


class GTP:
    def __init__(self, command):
        # Inherit the bridge's process group: the outer referee kills both.
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        self.responses = queue.Queue(maxsize=1)
        self.stopped = threading.Event()
        self.sequence = 0
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        try:
            while True:
                line = self.process.stdout.readline(MAX_LINE + 1)
                if not line:
                    raise EngineError("GTP engine closed stdout")
                if len(line) > MAX_LINE or not line.endswith(b"\n"):
                    raise EngineError("GTP response line exceeds 16 KiB or is truncated")
                if not self._deliver(line):
                    return
        except (OSError, EngineError) as error:
            self._deliver(error)

    def _deliver(self, item):
        while not self.stopped.is_set():
            try:
                self.responses.put(item, timeout=0.1)
                return True
            except queue.Full:
                pass
        return False

    def command(self, text, deadline):
        if time.monotonic() >= deadline:
            raise TimeoutError("GTP deadline reached")
        if "\n" in text or "\r" in text:
            raise EngineError("GTP command must be one line")
        self.sequence += 1
        self.process.stdin.write(f"{self.sequence} {text}\n".encode("ascii"))
        self.process.stdin.flush()
        header, lines, total = None, [], 0
        while True:
            try:
                raw = self.responses.get(timeout=max(0, deadline - time.monotonic()))
            except queue.Empty as error:
                raise TimeoutError("GTP response timed out") from error
            if isinstance(raw, Exception):
                raise raw
            total += len(raw)
            if total > MAX_MESSAGE:
                raise EngineError("GTP response exceeds 64 KiB")
            # GTP v2 ignores CR/control bytes and treats tabs as spaces.
            line = bytes(b for b in raw if (b >= 32 and b != 127) or b in (9, 10))
            line = line.replace(b"\t", b" ").decode("ascii").rstrip("\n")
            if header is None:
                if not line.strip():
                    continue
                header = re.fullmatch(r"([=?])([0-9]+)(?: +(.*))?", line)
                if not header or int(header[2]) != self.sequence:
                    raise EngineError("GTP response has a missing or stale command ID")
                lines.append(header[3] or "")
            elif not line.strip():
                result = "\n".join(lines).strip()
                if header[1] == "?":
                    raise EngineError(f"GTP {text.split()[0]} rejected: {result[:160]}")
                return result
            else:
                lines.append(line)

    def close(self):
        if self.process.poll() is None:
            try:
                self.command("quit", time.monotonic() + 0.2)
            except (OSError, ValueError, EngineError, TimeoutError):
                pass
            try:
                self.process.wait(timeout=0.2)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                try:
                    self.process.wait(timeout=0.2)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait()
        try:
            self.process.stdin.close()
        except OSError:
            pass
        self.stopped.set()
        self.reader.join(timeout=0.2)
        if not self.reader.is_alive():
            self.process.stdout.close()


class Bridge:
    def __init__(self, engine):
        self.engine = engine
        self.hello = None
        self.history = []
        self.has_time_settings = False

    def start(self, hello, deadline):
        if self.hello or hello.get("type") != "hello" or hello.get("protocol") != "yi-agent-v1":
            raise EngineError("expected one yi-agent-v1 hello")
        if hello["color"] not in ("black", "white") or hello["size"] not in (9, 13, 19) or not math.isfinite(hello["komi"]):
            raise EngineError("invalid board setup")
        if self.engine.command("protocol_version", deadline) != "2":
            raise EngineError("engine must implement GTP version 2")
        self.engine.command(f"boardsize {hello['size']}", deadline)
        self.engine.command("clear_board", deadline)
        self.engine.command(f"komi {hello['komi']}", deadline)
        self.has_time_settings = self.engine.command("known_command time_settings", deadline) == "true"
        self.hello = hello
        return {"type": "ready", "protocol": "yi-agent-v1", "id": hello["id"]}

    def move(self, turn):
        deadline = time.monotonic() + turn["timeLimitMS"] / 1000
        if not self.hello or turn.get("type") != "turn" or turn.get("protocol") != "yi-agent-v1" or any(
                turn[key] != self.hello[key] for key in ("gameId", "color", "size", "komi")):
            raise EngineError("turn does not match this engine's game")
        history = [vertex(move) for move in turn["history"]]
        if (len(history) != turn["revision"] or len(history) < len(self.history)
                or history[:len(self.history)] != self.history
                or turn["color"] != ("black" if len(history) % 2 == 0 else "white")):
            raise EngineError("history changed or turn color is inconsistent")
        for index in range(len(self.history), len(history)):
            color = "black" if index % 2 == 0 else "white"
            self.engine.command(f"play {color} {history[index]}", deadline)
            self.history.append(history[index])
        if self.has_time_settings:
            # GTP time controls use whole seconds; retain room for IPC overhead.
            seconds = max(1, int(max(0, deadline - time.monotonic()) * 0.8))
            self.engine.command(f"time_settings 0 {seconds} 1", deadline)
        move = vertex(self.engine.command(f"genmove {turn['color']}", deadline), resign=True)
        # genmove has already played the stone/pass inside the GTP engine.
        if move != "resign":
            self.history.append(move)
        # Preserve even an illegal choice for the authoritative referee to judge.
        return {"type": "move", "id": turn["id"], "revision": turn["revision"], "move": move}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check the engine before running a benchmark")
    parser.add_argument("--startup-timeout", type=float, default=4.0)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or not 0 < args.startup_timeout <= 60:
        parser.error("provide an engine argv after -- and a startup timeout in (0,60]")
    engine = GTP(command)
    bridge = Bridge(engine)
    try:
        if args.check:
            deadline = time.monotonic() + args.startup_timeout
            bridge.start({"type": "hello", "protocol": "yi-agent-v1", "id": "check", "color": "black", "size": 9, "komi": 6.5}, deadline)
            print(json.dumps({"protocol": 2, "name": engine.command("name", deadline), "version": engine.command("version", deadline)}))
            return
        while True:
            line = sys.stdin.buffer.readline(MAX_MESSAGE + 1)
            if not line:
                return
            if len(line) > MAX_MESSAGE or not line.endswith(b"\n"):
                raise EngineError("invalid JSONL request length")
            request = json.loads(line)
            reply = (bridge.move(request) if bridge.hello else
                     bridge.start(request, time.monotonic() + args.startup_timeout))
            print(json.dumps(reply, separators=(",", ":")), flush=True)
    finally:
        engine.close()


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, EngineError, TimeoutError) as error:
        print(f"GTP bridge: {error}", file=sys.stderr)
        sys.exit(2)
