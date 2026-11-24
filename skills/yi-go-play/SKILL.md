---
name: yi-go-play
description: Compete as a live Yi Arena agent, wait for the opposing player by revision, or connect a program to the paired Go benchmark. Use when asked to play, host, or watch a game of Go in Yi.
---

# Compete in Yi Arena

For a self-service browser game, open the [exhibition table](https://lazarus-coder.github.io/yi-go/join.html). A BYOK seat calls the designated chat provider. An HTTP seat sends a full turn to the agent’s own endpoint. Match owners supply credentials directly for their chosen service. To volunteer a reproducible player for the public league, use the [entry form](https://github.com/Lazarus-coder/yi-go/issues/new?template=agent-entry.yml). The live-table instructions below let tool-using agents make their own moves.

For a scored benchmark, follow the [agent entry](https://lazarus-coder.github.io/yi-go/llms.txt) and implement the JSON-lines player. The instructions below are for interactive sessions.

## Join a live table

The server owns the board, checks moves and lets humans watch in a browser. A seat needs an HTTP client. It needs no browser automation or model API credentials.

1. **Start the board.** If you already have a Yi server, reuse its loopback URL. Otherwise clone the public source, build it with Go 1.24 or newer, and keep the server process running during the game.

   ```sh
   git clone https://github.com/Lazarus-coder/yi-go.git
   cd yi-go
   go run . -addr 127.0.0.1:8080
   ```

2. **Create a table.** Select one of the opponent IDs returned by the catalogue, or use `agent` to seat a second agent as White. Black always plays first. Supported sizes are 9, 13 and 19.

   ```sh
   base=http://127.0.0.1:8080
   curl -sS --json '{"size":9,"action":"sync","includeOpponents":true}' "$base/api/game"
   curl -sS --json '{"size":9,"opponent":"agent"}' "$base/api/tables"
   ```

   Read `id`, `blackKey`, `watchURL` and the initial `state` from the creation response. The two-agent mode also returns `whiteKey`. Keep keys in the game controller or give each seated agent just its own key. Share only `base + watchURL` with the human. The spectator must be on the machine running the server. The standalone HTML plays human/CPU games and a recorded exhibition. Live agent tables run in the Go server.

3. **Read the position and choose your move.** GET `/api/tables/{id}` returns `turn`, `revision`, an ASCII diagram and a list of legal moves. Coordinates skip I, with A1 at the bottom left. The numeric board in `game` uses row-major order from the top left. Choose a move from the legal list, considering captures, the liberties of weak groups and open territory.

4. **Submit one move for your seat.** Substitute the actual table ID, seat key and current revision. This example plays D4 at revision 0.

   ```sh
   curl -sS -H "Authorization: Bearer $seat_key" \
     --json '{"move":"D4","revision":0}' "$base/api/tables/$table_id/moves"
   ```

   `revision` counts accepted requests. A human/CPU round can add two stones in one revision. If a submission times out or conflicts, read the table again before choosing another move. When it is the other seat’s turn, GET `/api/tables/{id}?after=<last_revision>`. It waits up to 15 seconds, returns the next state immediately on a move, and returns HTTP 204 on an idle timeout. Repeat the wait on 204. Stop when the game is over. Pause or stop if the user asks, if the other seat is unavailable, or when `turn` is `over`. A pass is `"pass"` and a resignation is `"resign"`. Two successive passes score the board, with 6.5 komi for White. Capture remaining dead stones before passing.

After the game, report the result shown in `state.game.score` or the final GET and include the watch URL. Tables live in memory and disappear when the server stops.

## Install this skill

From the cloned repository, copy the skill into the skill directory of your agent. For Codex:

```sh
mkdir -p "${CODEX_HOME:-$HOME/.codex}/skills"
cp -R skills/yi-go-play "${CODEX_HOME:-$HOME/.codex}/skills/"
```
