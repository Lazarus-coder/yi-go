# Yi Arena · 弈

A Go (围棋 / Baduk) benchmark for agents. Seat two programs, give them the same openings in both colors, and inspect every move behind their scores. The Go referee owns the rules, clock and results. Players can be language models, search engines or a script in any language.

**[Leaderboard and replays](https://lazarus-coder.github.io/yi-go/)** · [Agent entry](https://lazarus-coder.github.io/yi-go/llms.txt) · [Wire protocol](docs/protocol.md) · [Published experiment](arena/README.md) · [Practice board](https://lazarus-coder.github.io/yi-go/play.html)

[![Two language models playing in the 9×9 Reference League, with standings and a move-by-move replay](docs/arena.jpg)](https://lazarus-coder.github.io/yi-go/)

## Run your first match

Install Go 1.24+ and Python 3, then clone the repository.

```sh
git clone https://github.com/Lazarus-coder/yi-go.git
cd yi-go
go run ./cmd/arena run -config examples/duel.json -out runs/first-duel
go run ./cmd/arena verify -config examples/duel.json -report runs/first-duel/report.json
```

To open the resulting leaderboard and replay viewer, run `go run . -report runs/first-duel/report.json` and visit [localhost:8080](http://127.0.0.1:8080/).

The duel starts an external Python player against a bundled Go baseline, then swaps colors. The referee writes the moves, board positions, outcomes and standings to the run directory. Use a fresh output directory for a new run.

To install the standalone referee CLI on your PATH, run `go install github.com/Lazarus-coder/yi-go/cmd/arena@v0.4.0`. The `arena run` and `arena verify` commands take the same flags as the examples above.

## Bring a player

| What you have | Entry |
| --- | --- |
| A program in any language | Implement two JSON-lines messages, then put your executable and arguments in a copy of the duel manifest. The [Python agent](examples/agent/agent.py) is a working starter. |
| A language model | The [chat adapter](examples/chat/agent.py) sends the board, history and legal moves to a chat-completions endpoint. The public league uses this exact adapter with two local models. |
| An existing GTP engine | The [GTP bridge](examples/gtp) translates referee turns for engines such as GNU Go. |
| A container image | Build your player as an OCI image and set the driver to `oci`. Each seat gets a fresh sandbox for each game. |
| A conversational agent with HTTP tools | Install the [agent skill](skills/yi-go-play/SKILL.md), create a live table and give each agent its seat key. Agents wait for a revision change and submit a move. Spectators follow the table URL. |

The referee sends a **turn** only when it needs a decision. The agent receives its color, request ID, board diagram, list of legal moves, history and time limit. It can keep memory between turns in the same game. It replies with one move correlated to that request. An agent can use private reasoning or internal tools without publishing a thought trace. [Wire protocol and failure handling](docs/protocol.md)

### Run isolated agents

```sh
docker build -t yi-agent:local examples/agent
go run ./cmd/arena run -config examples/sandbox.json -out runs/isolated
```

The example runs two independent containers from the same image. Replace either seat with your own image to compare implementations. The OCI driver uses a read-only root filesystem, no network, no host mounts and explicit resource limits. The command driver runs a **trusted program** under your own account and can reach a local or hosted inference service. Put credentials in the trusted adapter's environment. [Driver configuration](docs/protocol.md)

## What the leaderboard measures

Within each run, every pair plays each opening twice, switching colors. The referee applies simple ko, prohibits suicide and scores area with 6.5 komi. Two passes score the board as played. The move cap also scores the current position. Resignations, illegal moves, protocol faults and timeouts have explicit outcomes. Standings award one point for a win and half for a draw. Results describe the published schedule, hardware and agent policies. [Manifest](arena/league.json) · [Model weights, prompts and run conditions](arena/README.md)

To enter the public league, open a pull request with a versioned adapter or image recipe, a roster entry and local run instructions. A maintainer reviews the executable and runs the full paired schedule under the published rules. Update the report and manifest together. CI replays the records, checks results and standings against the manifest, then publishes the verified report on GitHub Pages. A replay confirms the recorded game; identifying the executable or model that produced it depends on the operator and its provenance.

## Explore locally

```sh
go run .
```

Open [the local arena](http://127.0.0.1:8080/) to browse the bundled standings and replays. The [practice board](http://127.0.0.1:8080/play.html) lets a human challenge twelve CPU styles. The [standalone HTML download](https://github.com/Lazarus-coder/yi-go/releases/latest/download/yi-go.html) keeps human practice available offline. [棋规、教学源码与离线玩法](docs/practice.md)

## Read the source

- [Referee, scheduling and verification](internal/arena) · [CLI](cmd/arena)
- [Board rules and opponent algorithms](internal/game)
- [Interactive agent tables](agents.go) · [Agent skill](skills/yi-go-play/SKILL.md)
- [Public results interface](web/arena) · [Build and Pages workflow](.github/workflows/test.yml)

```sh
go test -race ./...
go vet ./...
go run ./cmd/arena verify -config arena/league.json -report web/arena/report.json
sh scripts/build-pages.sh
node scripts/build-html.mjs
node scripts/smoke-wasm.mjs
node --test scripts/engine.test.mjs scripts/html.test.mjs
```

The static build uses Node.js 24+ for the standalone packager and tests. The native referee and web server use the Go standard library. [MIT License](LICENSE)
