# Use an existing GTP engine

`agent.py` connects a GTP v2 engine to the arena's JSONL player protocol. It uses
Python's standard library and keeps one engine process alive for each game.
GNU Go and KataGo can use this path without implementing a new player protocol.

First check the exact executable and arguments outside a scored league:

```sh
python3 examples/gtp/agent.py --check -- gnugo --mode gtp --level 1 --chinese-rules
```

The check initializes GTP v2, a 9×9 board and komi, then prints the engine name and
version. A missing executable, invalid model/configuration or incompatible GTP
implementation must be fixed before benchmarking. The arena cannot infer the
cause of an arbitrary bridge process's startup exit. Engine binaries and model
weights are not bundled or installed by this example.

Use an external player's existing `driver` configuration:

```json
{
  "mode": "command",
  "command": ["python3", "examples/gtp/agent.py", "--", "gnugo", "--mode", "gtp", "--level", "1", "--chinese-rules"],
  "startupTimeoutMS": 10000
}
```

For KataGo, replace the engine argv with your installed GTP invocation, for
example `katago gtp -model /path/model.bin.gz -config /path/gtp.cfg`. Select rules,
hardware backend and resource limits in that engine's configuration. Declare its
actual version, model, policy and limits in the tournament's participant record.
These are trusted local commands with host access. To isolate an untrusted
engine, package Python, this bridge and the engine in an OCI image and use the
arena's existing OCI driver; the bridge adds no mounts or network permissions.

The bridge sets board size, clears the board and sets komi once. It replays every
unseen history move with its recorded color, including the preset opening and
passes. GTP `genmove` already applies the engine's own move, so that move is
remembered and never replayed. A changed prefix is rejected rather than silently
resetting engine memory. A resignation is forwarded as `resign`.

Every GTP command has an increasing ID. Missing/stale IDs, error responses,
malformed output and excessive responses stop the bridge; no substitute move is
invented. A syntactically valid but illegal generated move is forwarded unchanged
for the Go referee to adjudicate. The referee, not GTP `final_score`, owns scoring
and the move cap; engine rule or dead-stone assumptions may differ.

Use a turn budget of at least 2 seconds unless you have tuned the engine for a
shorter budget. When supported, the bridge supplies a one-stone GTP time setting
using 80% of remaining time, rounded down to whole seconds with a one-second
minimum. The arena's original deadline still applies, including synchronization
and IPC. Engines that ignore GTP clocks need their own search limits. Reads are
bounded to 16 KiB per GTP line and 64 KiB per response. On stdin EOF the bridge
requests `quit`, then terminates a nonresponsive engine. The engine inherits the
bridge's process group so outer arena cancellation removes both.

Run the portable regressions and optional real-engine integration:

```sh
python3 -m unittest discover -s examples/gtp -p 'test_*.py' -v
YI_GTP_COMMAND='["gnugo","--mode","gtp","--level","1","--chinese-rules"]' \
  go test ./internal/arena -run '^TestExternalGTPReference$' -v
```

The first command tests exact opening/history replay, both colors, pass/resign,
IDs, rejected commands, illegal choices, output bounds and process cleanup with
a real stub subprocess. The second runs two color-swapped referee games between
real engine processes and verifies their recorded moves/results. It is skipped
when `YI_GTP_COMMAND` is absent; stub success is not a real-engine claim.

Protocol reference: [GTP v2 specification](https://www.lysator.liu.se/~gunnar/gtp/gtp2-spec-draft2/gtp2-spec.html).
Engine setup: [GNU Go](https://www.gnu.org/software/gnugo/),
[KataGo GTP documentation](https://github.com/lightvector/KataGo/blob/master/docs/GTP_Extensions.md).
