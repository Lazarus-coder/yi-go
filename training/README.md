# Training the move policy

On 9×9, the neural opponent won **9 of 16** against the v0.5.1 top opponent at the same 300 ms target. It won **7 of 8** against the same engine with the network disabled and both sides fixed at depth two. Each opening was played in both colors. [Match records and reproducer](matches.zip)

The top opponent scores each candidate with 34 board features, 32 ReLU units and one output. All board sizes and both colors share its 1,152 weights and biases. Go and WebAssembly load the same [weights](../internal/game/policy_weights.json).

GNU Go 3.8 played both colors at level 1 after seeded openings of 1–7 stones. The first 120 games supplied 8,490 training positions. The final 24 games were held out whole. Each position contributes one cross-entropy loss over the candidate moves. [Training games](games.jsonl.gz) · [Feature definitions](../internal/game/policy_features.go)

| Held-out board | Games | Unseen positions | Neural top-1 | Heuristic top-1 | Uniform top-1 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 9×9 | 12 | 383 | 36.8% | 16.4% | 2.0% |
| 13×13 | 8 | 763 | 29.5% | 9.8% | 1.0% |
| 19×19 | 4 | 586 | 18.4% | 4.4% | 0.4% |
| All | 24 | 1,732 | 27.4% | 9.5% | 1.0% |

Top-1 means the first-ranked candidate was the teacher’s move. The previous heuristic and the neural scorer ranked identical candidate sets. The uniform column is the mean of one divided by the number of candidates. Of 1,816 held-out point moves, 1,798 were eligible. The comparison excluded 42 passes, 18 moves outside the candidate set, and 66 positions whose candidate features also appeared in training. All 19×19 training and evaluation games stopped at 152 plies, so those examples cover the opening and middlegame.

<details>
<summary>Reproduce the weights</summary>

From a checkout with Go 1.24+, Python 3 and NumPy (trained with 2.0.2), run

```sh
mkdir -p runs/policy
gzip -dc training/games.jsonl.gz > runs/policy/games.jsonl
YI_POLICY_GAMES="$PWD/runs/policy/games.jsonl" \
YI_POLICY_EXAMPLES="$PWD/runs/policy/features.jsonl" \
go test ./internal/game -run '^TestExportPolicyTraining$' -count=1
VECLIB_MAXIMUM_THREADS=1 OPENBLAS_NUM_THREADS=1 python3 training/fit.py \
  --games runs/policy/games.jsonl --examples runs/policy/features.jsonl \
  --output runs/policy/weights.json
```

The exporter runs the same feature code as the engine. The trainer fits 30 epochs with Adam, folds input normalization into the first layer, and exports one file. Last-digit differences in linear algebra are possible across platforms.

To regenerate the positions, run `python3 training/games.py --gnugo /path/to/gnugo --output runs/policy/regenerated.jsonl`. The decompressed training corpus has SHA-256 `b43218263b8fb276ab8ac0ea4b66494da44644ae52595cb8657f3bcfab2234d8`.

</details>
