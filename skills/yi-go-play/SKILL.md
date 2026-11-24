---
name: yi-go-play
description: Play Go (围棋, Baduk) as a seated agent against the built-in CPU or another agent, and give a human the live local spectator URL. Use when asked to play, host, or watch a game of Go in Yi.
---

# Play Go at a local table

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
   curl -sS --json '{"size":9,"opponent":"club-fighter"}' "$base/api/tables"
   ```

   Read `id`, `blackKey`, `watchURL` and the initial `state` from the creation response. The two-agent mode also returns `whiteKey`. Keep keys in the game controller or give each seated agent just its own key. Share only `base + watchURL` with the human. The spectator must be on the machine running the server. The standalone HTML plays human/CPU games and a recorded exhibition. Live agent tables run in the Go server.

3. **Read the position and choose your move.** GET `/api/tables/{id}` returns `turn`, `revision`, an ASCII diagram and a list of legal moves. Coordinates skip I, with A1 at the bottom left. The numeric board in `game` uses row-major order from the top left. Choose a move from the legal list, considering captures, the liberties of weak groups and open territory.

4. **Submit one move for your seat.** Substitute the actual table ID, seat key and current revision. This example plays D4 at revision 0.

   ```sh
   curl -sS -H "Authorization: Bearer $seat_key" \
     --json '{"move":"D4","revision":0}' "$base/api/tables/$table_id/moves"
   ```

   `revision` counts accepted requests. A human/CPU round can add two stones in one revision. If a submission times out or conflicts, read the table again before choosing another move. For two agents, wait for your color to move. Pause or stop if the user asks, if the other seat is unavailable, or when `turn` is `over`. A pass is `"pass"` and a resignation is `"resign"`. Two successive passes score the board, with 6.5 komi for White. Capture remaining dead stones before passing.

After the game, report the result shown in `state.game.score` or the final GET and include the watch URL. Tables live in memory and disappear when the server stops.
