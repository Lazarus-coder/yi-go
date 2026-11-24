# 9×9 Reference League

This is an observed match series between two local language-model agents and one deterministic Go baseline. The referee played every pairing in both colors from an empty board and from opposite corners (C7, G3). Each entrant plays eight games, four with Black and four with White. The twelve games are the whole run. [Standings and replays](https://lazarus-coder.github.io/yi-go/) · [Raw report](../web/arena/report.json) · [Roster and openings](league.json)

## Result · 24 November 2026

| Player | Wins | Losses | Forfeits |
| --- | ---: | ---: | ---: |
| Yi Club Fighter | 8 | 0 | 0 |
| SmolLM2 360M Instruct | 3 | 5 | 0 |
| Qwen2.5 0.5B Instruct | 1 | 7 | 0 |

The built-in baseline won all eight games against the language models. SmolLM2 won three of the four direct model games. All four direct games reached the 144-ply cap and used the published area-scoring rule. The eight baseline games finished by consecutive passes. [Replay the first model game](https://lazarus-coder.github.io/yi-go/#game=g-000001&ply=0)

## Who played

| Entrant | Version | Decision procedure |
| --- | --- | --- |
| Qwen2.5 0.5B Instruct | Q4_K_M, upstream revision `9217f5d` | One local model call per turn through the shared chat adapter. |
| SmolLM2 360M Instruct | Q8_0, upstream revision `593b5a2` | The same adapter, prompt, observation and decoding policy. |
| Yi Club Fighter | Built-in `club-fighter`, heuristic-v1 | The Go engine scores legal candidate moves using captures, threats and shape. |

Both language models use their published instruction templates. The adapter sends the public board, side, rules, captures, history, remaining move budget and legal moves. Greedy decoding uses temperature 0, a 32-token reply limit and a JSON Schema enumerating legal choices. The selected move comes from the model. The adapter does no tactical search or strategic post-processing. This experiment therefore tests move selection with constrained output; generating valid coordinate syntax unaided would be a different experiment. [Exact adapter and prompt](../examples/chat/agent.py)

These are small models and one specialist baseline. The paired games describe this field under these conditions. Different hardware, quantization, decoding, prompts or openings can change the results. A color pair on the same opening is a matched comparison, not two independent samples of general Go strength.

## Rules and resources

- 9×9 board, simple ko, no suicide, area scoring, White receives 6.5 komi. Play out dead stones. Two passes score the remaining position.
- 10 seconds per turn. After 144 total plies (including the opening), score the current board. A capped game is labeled separately in the match list.
- Resigning loses immediately. A player's timeout, crash, malformed response or illegal move is recorded as a forfeit. An infrastructure failure reported by the referee aborts publication of the unfinished run.
- Same Apple M3 machine with 16 GiB of memory, macOS 26.5.2. Games ran sequentially. Both model servers used llama.cpp build `b12669` (commit `30ec35ae2`), one slot, context length 4096, four CPU threads and its default macOS accelerator selection. The two Python adapters ran as trusted commands against loopback. This run did not measure energy, token cost or equal FLOPs.

## Re-run with the same weights

Use the architecture-appropriate server from the [pinned llama.cpp release](https://github.com/ggml-org/llama.cpp/releases/tag/b12669). Download the two GGUF files from their upstream projects.

| Local filename | Upstream file | SHA-256 |
| --- | --- | --- |
| `qwen.gguf` | [Qwen Q4_K_M](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/blob/9217f5db79a29953eb74d5343926648285ec7e67/qwen2.5-0.5b-instruct-q4_k_m.gguf) | `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db` |
| `smol.gguf` | [SmolLM2 Q8_0](https://huggingface.co/HuggingFaceTB/SmolLM2-360M-Instruct-GGUF/blob/593b5a2e04c8f3e4ee880263f93e0bd2901ad47f/smollm2-360m-instruct-q8_0.gguf) | `48ab3034d0dd401fbc721eb1df3217902fee7dab9078992d66431f09b7750201` |

In separate terminals, launch the two local servers:

```sh
llama-server -m qwen.gguf --alias qwen2.5-0.5b-instruct --host 127.0.0.1 --port 18251 -c 4096 -np 1 -t 4
```

```sh
llama-server -m smol.gguf --alias smollm2-360m-instruct --host 127.0.0.1 --port 18252 -c 4096 -np 1 -t 4
```

From the repository root:

```sh
go run ./cmd/arena run -config arena/league.json -out runs/reference
go run ./cmd/arena verify -config arena/league.json -report runs/reference/report.json
go run . -report runs/reference/report.json
```

For a different model service, configure the adapter with a base URL ending in `/v1` and a model ID returned by its `/models` endpoint. The chat endpoint must support a strict JSON object with a string enum. If it needs a key, use `--token-env NAME` to name the variable in the trusted adapter environment.

## Publishing an entry

1. Add a versioned player recipe and describe its model, prompt or search policy, decoding settings and execution requirements. For an OCI entrant, record the immutable image digest in its version or recipe. Keep private credentials outside the repository.
2. Open a pull request with the recipe and an example local command. The organizer reviews the code and resource limits before running it in a league.
3. Run the **entire paired schedule** with the reviewed manifest. Preserve the records for the run. Copy its verified `report.json` to `web/arena/report.json` together with the matching manifest.
4. CI replays the games, checks the declared terminal states, recalculates standings and publishes the static showcase after merge. A visitor can download the report and run the same verifier without obtaining model weights. Git history and release assets preserve earlier published reports.

The replay establishes legal play and consistency of the reported outcomes. An agent self-report, a record hash, or a replay alone cannot certify the identity of an external model or that a recorded timeout really occurred. The organizer is responsible for those observations.
