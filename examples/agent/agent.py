#!/usr/bin/env python3
"""Tiny persistent JSONL agent. Python standard library only; no network."""
import json
import os
import random
import socket
import sys

args = sys.argv[1:]
if args and args[0] == "--verify-isolation":
    args.pop(0)
    assert os.getuid() != 0 and "YI_HOST_SENTINEL" not in os.environ
    with open("/proc/self/mountinfo") as mounts:
        assert any(parts[4] == "/" and "ro" in parts[5].split(",")
                   for parts in (line.split() for line in mounts))
    assert {name for _, name in socket.if_nameindex()} <= {"lo"}
    try:
        with open("/arena-write-probe", "w") as probe:
            probe.write("should not be writable")
    except OSError:
        pass
    else:
        raise RuntimeError("container root is writable")
    try:
        connection = socket.create_connection(("1.1.1.1", 443), timeout=0.5)
    except OSError:
        pass
    else:
        connection.close()
        raise RuntimeError("container network is available")
rng = random.Random(int(args[0]) if args else 0)
turns = 0
for line in sys.stdin:
    request = json.loads(line)
    if request["type"] == "hello":
        response = {"type": "ready", "protocol": "yi-agent-v1", "id": request["id"]}
    else:
        turns += 1  # Memory survives between this player's turns, not between games.
        choices = [move for move in request["legal"] if move not in ("pass", "resign")]
        move = rng.choice(choices) if choices and turns <= request["size"] ** 2 else "pass"
        response = {"type": "move", "id": request["id"], "revision": request["revision"], "move": move}
    print(json.dumps(response, separators=(",", ":")), flush=True)
