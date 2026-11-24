# External agent protocol

An external player is one persistent process per seat per game. It may keep
conversation history, a search tree or other memory between turns. A new game
starts a fresh process. The Go referee owns the rules, timing and final result.

For a browser exhibition, register the full URL of a turn handler in either seat at the [open table](https://lazarus-coder.github.io/yi-go/join.html). HTTP players receive one self-contained `turn` per POST and reply with a correlated `move`. See the [HTTP contract, CORS settings and JSONL bridge](../examples/http/README.md).

## Transport configuration

Use an explicit `driver` in an external player's tournament entry:

```json
{"mode":"command","command":["python3","examples/agent/agent.py","1"],"startupTimeoutMS":5000}
```

Command mode runs trusted code with the caller's working directory, environment,
filesystem access and network. Arguments are passed directly, without a shell.
Use this mode for local tools or an LLM provider bridge that needs credentials
and network access. Paths are relative to the arena's working directory, not the
configuration file. Do not place secrets in command-line arguments or reports.

For an already built local container image:

```json
{"mode":"oci","runtime":"docker","image":"yi-agent:local","command":["2"],"memoryMB":256,"cpus":1,"startupTimeoutMS":10000}
```

`runtime` is `docker` (default) or `podman`. The runner never starts a daemon,
pulls images, or falls back from OCI to trusted command mode. `command` overrides
the image's CMD; its ENTRYPOINT remains in effect.

OCI execution enforces no network, UID/GID 65534, a read-only root, no Linux
capabilities, no-new-privileges, 64 PIDs, a memory/swap ceiling and a CPU limit.
Only `/tmp` is writable: a 16 MiB, noexec/nosuid/nodev tmpfs. No host environment,
socket or directory is passed into the container. Image-declared VOLUME mounts
are rejected. Image-baked environment remains available. OCI isolation relies
on the installed runtime and its host kernel; trusted command mode is not a
security sandbox. The runtime's normal host connection/configuration is used.

## JSONL exchange

stdin/stdout contain one UTF-8 JSON object per newline. Flush every response.
Responses must be at most 64 KiB and have exactly the documented keys; duplicate
keys, extra output, stale IDs/revisions and malformed notation are faults.
Diagnostics belong on stderr, which the runner discards rather than including
possibly sensitive provider output in public reports.

The runner first sends a `hello`:

```json
{"type":"hello","protocol":"yi-agent-v1","id":"private-session:hello","gameId":"g000001","color":"black","size":9,"komi":6.5,"players":{"black":"alpha","white":"beta"},"maxMoves":200,"turnTimeoutMS":1000,"revision":2,"remainingMoves":198}
```

Reply exactly:

```json
{"type":"ready","protocol":"yi-agent-v1","id":"private-session:hello"}
```

Both seats receive the same public `gameId` and participant IDs as the report;
the ID is scoped to that tournament run. The opaque request `id` is distinct for
each seat and request. Echo it unchanged. The `revision` counts played plies,
including the supplied opening and passes. Opening plies count toward `maxMoves`.

Each `turn` has these fields:

| Field | Meaning |
| --- | --- |
| `type`, `protocol`, `id`, `gameId` | `turn`, `yi-agent-v1`, unique request ID, public game ID |
| `color`, `players` | Your color (`black`/`white`) and both public participant IDs |
| `revision`, `history` | Current ply count and complete coordinate/pass history |
| `size`, `komi` | Board size and white's 6.5-point compensation |
| `ascii` | Labeled board: X black, O white, . empty; column I is skipped |
| `board` | Row-major integers: 0 empty, 1 black, 2 white; top row first |
| `legal` | Legal coordinates plus `pass` and `resign` |
| `captures` | `{black,white}` numbers of stones taken |
| `rules` | `{ko:"simple",scoring:"area",deadStones:"not adjudicated"}` |
| `maxMoves`, `remainingMoves` | Tournament cap and plies left before cap adjudication |
| `turnTimeoutMS`, `timeLimitMS` | Configured turn budget and actual remaining response time |

Reply with one correlated choice, for example:

```json
{"type":"move","id":"private-session:1","revision":2,"move":"D4"}
```

`move` may be a coordinate, `pass` or `resign`. The referee checks legality
against its own board. No explanations, Markdown, extra JSON lines or unsolicited
responses belong on stdout. Two consecutive passes end the game; the move cap
uses the tournament's documented area estimate, with no dead-stone adjudication.

Startup readiness has its own deadline (5 seconds by default). Each turn uses
the referee deadline, bounded by the configured turn budget. Timeout, malformed
protocol and a started agent's crash are agent faults. A missing executable,
unavailable OCI daemon/image, failed container creation or entrypoint setup, and
failed container cleanup are infrastructure errors: the run must remain unscored.
Operator cancellation is also unscored. On close/cancel, the runner closes pipes,
terminates the Unix process group (Windows uses taskkill /T), and force-removes
the OCI container. Trusted programs that deliberately escape process groups are
not contained; use OCI for untrusted agents.

## Reference agent and verification

`examples/agent/agent.py` uses only Python's standard library. Its optional
integer argument is its RNG seed. It keeps a turn counter and RNG state for one
game, chooses from the provided legal list, and eventually passes. It is an
interface example, not a strong Go opponent.

```sh
go test ./internal/arena -run 'TestTwoRealPythonAgents|TestPersistentAgent|TestAgentStartup|TestCancellation|TestPublicGameIdentity' -v
docker build -t yi-agent:local examples/agent
YI_TEST_OCI_IMAGE=yi-agent:local go test ./internal/arena -run '^TestOCIPlayer$' -v
```

The Python integration test plays a complete game between two real processes.
The OCI test is opt-in and expects an already available runtime; it verifies
unprivileged execution, missing host sentinel environment, denied root writes,
blocked outbound networking, a legal move and successful cleanup. Use
`YI_TEST_OCI_RUNTIME=podman` with a locally built Podman image. Default tests do
not imply that OCI has been exercised on a machine without a runtime.

Runtime controls follow the official [Docker create/run reference](https://docs.docker.com/reference/cli/docker/container/run/)
and [Podman run reference](https://docs.podman.io/en/latest/markdown/podman-run.1.html).
