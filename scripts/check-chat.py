"""Exercise the shipped chat adapter against the real referee with a local test endpoint."""
import json
import os
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


class Model(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send_json(self, body):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def do_GET(self):
        if self.path == "/unexpected":
            self.server.followed_redirect = True
            return self.send_json({})
        assert self.path == "/v1/models"
        assert self.headers.get("Authorization") is None
        self.send_json({"data": [{"id": "fixture"}]})

    def do_POST(self):
        if self.path == "/redirect/chat/completions":
            self.send_response(302)
            self.send_header("Location", "/unexpected")
            self.end_headers()
            return
        if self.path == "/unexpected":
            self.server.followed_redirect = True
            return self.send_json({})
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path == "/private/v1/chat/completions":
            assert self.headers.get("Authorization") == "Bearer local-fixture"
            assert "response_format" not in request
            return self.send_json({"choices": [{"message": {"content": "pass"}}]})
        assert self.path == "/v1/chat/completions" and self.headers.get("Authorization") is None
        observation = json.loads(request["messages"][-1]["content"])
        schema = request["response_format"]["json_schema"]["schema"]
        assert schema["properties"]["move"]["enum"] == observation["legal"]
        assert observation["remainingMoves"] > 0 and observation["ascii"]
        self.send_json({"choices": [{"message": {"content": '{"move":"pass"}'}}]})


with ThreadingHTTPServer(("127.0.0.1", 0), Model) as server, tempfile.TemporaryDirectory() as directory:
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        manifest = json.loads(Path("examples/duel.json").read_text())
        manifest["maxMoves"] = 4
        manifest["players"][0]["driver"]["command"] = [
            "python3", "examples/chat/agent.py", "--model", "fixture",
            "--base-url", f"http://127.0.0.1:{server.server_port}/v1",
        ]
        config = Path(directory, "config.json")
        config.write_text(json.dumps(manifest))
        subprocess.run(["go", "run", "./cmd/arena", "run", "-config", str(config),
                        "-out", directory], check=True, timeout=60)
        games = json.loads(Path(directory, "report.json").read_text())["games"]
        assert len(games) == 2 and all(g["result"]["reason"] == "passes" for g in games)
        for game in games:
            offset = int(game["white"] == manifest["players"][0]["id"])
            chosen = game["moves"][offset::2]
            assert chosen and all(move == {"x": -1, "y": -1} for move in chosen)

        # Run the user-facing keyed manifest as written, including both color assignments.
        root = f"http://127.0.0.1:{server.server_port}"
        env = dict(os.environ, YI_CHAT_BASE_URL=root + "/private/v1",
                   YI_CHAT_MODEL="fixture", YI_CHAT_KEY="local-fixture")
        output = Path(directory, "private")
        subprocess.run(["go", "run", "./cmd/arena", "run", "-config", "examples/byok.json", "-out", str(output)],
                       env=env, check=True, timeout=60)
        report = (output / "report.json").read_text()
        assert env["YI_CHAT_KEY"] not in report and root not in report
        games = json.loads(report)["games"]
        assert len(games) == 2 and all(game["result"]["reason"] == "passes" for game in games)

        # A redirect cannot forward the configured credential, including to another URL on this host.
        server.followed_redirect = False
        event = dict(type="turn", color="black", size=9, komi=6.5, ascii=".", history=[],
                     legal=["pass"], captures={}, rules={}, remainingMoves=2, timeLimitMS=1000)
        run = subprocess.run(["python3", "examples/chat/agent.py", "--plain", "--token-env", "YI_CHAT_KEY"],
                             input=json.dumps(event) + "\n", capture_output=True, text=True, timeout=5,
                             env=dict(env, YI_CHAT_BASE_URL=root + "/redirect"))
        assert run.returncode != 0 and not server.followed_redirect
        assert env["YI_CHAT_KEY"] not in run.stderr + run.stdout
    finally:
        server.shutdown()
        thread.join()
