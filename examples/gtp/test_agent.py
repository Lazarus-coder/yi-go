import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

from agent import Bridge, EngineError, GTP


def fake_engine(mode, log):
    moves, generated = [], 0
    with open(log, "a", buffering=1) as transcript:
        for line in sys.stdin:
            number, command = line.strip().split(" ", 1)
            transcript.write(command + "\n")
            verb, *args = command.split()
            result, success = "", True
            if verb == "protocol_version":
                result = "2"
            elif verb in ("name", "version"):
                result = "stub"
            elif verb == "known_command":
                result = "true"
            elif verb == "clear_board":
                moves = []
            elif verb == "play":
                color, move = args
                if mode == "error" or (move != "pass" and move in moves):
                    result, success = "illegal move", False
                else:
                    moves.append(move)
            elif verb == "genmove":
                if mode == "hang":
                    time.sleep(10)
                generated += 1
                result = ["D4", "pass", "resign"][min(generated - 1, 2)]
                if mode == "illegal":
                    result = "C3"
                if mode == "oversized":
                    result = "x" * (17 * 1024)
                if mode == "multiline":
                    result = "D4\nnot-a-move"
                if result != "resign":
                    moves.append(result)
            if mode == "wrong-id":
                number = str(int(number) + 1)
            # CRLF, pre-response whitespace and numbered multiline framing.
            print(f"\r\n{'=' if success else '?'}{number} {result}\r\n\r", flush=True)
            if verb == "quit":
                return


def hello(color="black"):
    return {"type": "hello", "protocol": "yi-agent-v1", "id": "hello-1",
            "gameId": "paired-game", "color": color, "size": 9, "komi": 6.5}


def turn(history, color="black"):
    return {"type": "turn", "protocol": "yi-agent-v1", "id": "move-" + str(len(history)),
            "gameId": "paired-game", "color": color, "size": 9, "komi": 6.5,
            "history": history, "revision": len(history), "timeLimitMS": 1000,
            "legal": ["pass", "resign"]}


class BridgeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.log = str(Path(self.temp.name) / "gtp.log")

    def command(self, mode="normal"):
        return [sys.executable, __file__, "--fake-engine", mode, self.log]

    def bridge(self, mode="normal", color="black"):
        engine = GTP(self.command(mode))
        self.addCleanup(engine.close)
        bridge = Bridge(engine)
        ready = bridge.start(hello(color), time.monotonic() + 2)
        self.assertEqual(ready, {"type": "ready", "protocol": "yi-agent-v1", "id": "hello-1"})
        return bridge

    def test_opening_both_colors_and_passes_are_applied_once(self):
        bridge = self.bridge()
        history = ["C3", "G7", "pass", "D5"]
        self.assertEqual(bridge.move(turn(history))["move"], "D4")
        history += ["D4", "E5"]
        self.assertEqual(bridge.move(turn(history))["move"], "pass")
        history += ["pass", "F6"]
        self.assertEqual(bridge.move(turn(history))["move"], "resign")
        commands = Path(self.log).read_text().splitlines()
        self.assertEqual([line for line in commands if line.startswith("play ")],
                         ["play black C3", "play white G7", "play black pass",
                          "play white D5", "play white E5", "play white F6"])
        self.assertEqual(commands.count("genmove black"), 3)

    def test_white_side_and_black_pass(self):
        bridge = self.bridge(color="white")
        self.assertEqual(bridge.move(turn(["C3"], "white"))["move"], "D4")
        self.assertEqual(bridge.move(turn(["C3", "D4", "pass"], "white"))["move"], "pass")
        commands = Path(self.log).read_text().splitlines()
        self.assertEqual([line for line in commands if line.startswith("play ")], ["play black C3", "play black pass"])

    def test_generated_illegal_choice_is_preserved_for_referee(self):
        bridge = self.bridge("illegal")
        self.assertEqual(bridge.move(turn(["C3", "pass"]))["move"], "C3")

    def test_changed_history_is_rejected(self):
        bridge = self.bridge()
        bridge.move(turn([]))
        with self.assertRaises(EngineError):
            bridge.move(turn(["A1", "E5"]))

    def test_protocol_error_and_bounded_reads(self):
        engine = GTP(self.command("wrong-id"))
        with self.assertRaises(EngineError):
            Bridge(engine).start(hello(), time.monotonic() + 2)
        engine.close()
        self.assertFalse(engine.reader.is_alive())
        for mode in ("error", "oversized", "multiline"):
            with self.subTest(mode=mode):
                bridge = self.bridge(mode)
                with self.assertRaises(EngineError):
                    bridge.move(turn(["C3", "pass"]))
        bridge = self.bridge("hang")
        request = turn([])
        request["timeLimitMS"] = 30
        started = time.monotonic()
        with self.assertRaises(TimeoutError):
            bridge.move(request)
        self.assertLess(time.monotonic() - started, 0.5)
        bridge.engine.close()
        self.assertIsNotNone(bridge.engine.process.poll())

    def test_actual_jsonl_bridge_process(self):
        payload = "\n".join(json.dumps(message) for message in (hello(), turn(["C3", "pass"]))) + "\n"
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("agent.py")), "--", *self.command()],
                                input=payload, text=True, capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        replies = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual(len(replies), 2)
        self.assertEqual(replies[1], {"type": "move", "id": "move-2", "revision": 2, "move": "D4"})
        self.assertEqual(Path(self.log).read_text().splitlines()[-1], "quit")


if __name__ == "__main__":
    if sys.argv[1:2] == ["--fake-engine"]:
        fake_engine(sys.argv[2], sys.argv[3])
    else:
        unittest.main()
