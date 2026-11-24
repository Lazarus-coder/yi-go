"""Exercise the shipped chat adapter against the real referee with a local test endpoint."""
import json
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
        assert self.path == "/v1/models"
        self.send_json({"data": [{"id": "fixture"}]})

    def do_POST(self):
        assert self.path == "/v1/chat/completions"
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
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
    finally:
        server.shutdown()
        thread.join()
