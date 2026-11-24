"""Fit a small shared move scorer to per-position GNU Go move choices."""
import argparse
import hashlib
import json
import math
from collections import Counter, defaultdict
from pathlib import Path

import numpy as np


def load(path):
    rows = []
    with path.open() as source:
        for line in source:
            item = json.loads(line)
            features = np.asarray(item["features"], dtype=np.float32)
            target = item["target"]
            if not 0 <= target < len(features) or features.ndim != 2:
                raise ValueError("invalid position from feature exporter")
            rows.append((item["game"], item["size"], target, features))
    return rows


def train(rows, epochs, seed):
    rng = np.random.default_rng(seed)
    inputs = rows[0][3].shape[1]
    count = sum(len(features) for _, _, _, features in rows)
    mean = sum(features.sum(axis=0, dtype=np.float64) for _, _, _, features in rows) / count
    moment = sum((features ** 2).sum(axis=0, dtype=np.float64) for _, _, _, features in rows) / count
    scale = np.maximum(np.sqrt(np.maximum(moment - mean**2, 0)), 0.05).astype(np.float32)
    mean = mean.astype(np.float32)
    # Fuse standardization into the first layer when exporting the model below.
    positions = [(target, (features - mean) / scale) for _, _, target, features in rows]
    width = 32
    weights = rng.normal(0, math.sqrt(2 / inputs), (inputs, width)).astype(np.float32)
    bias = np.zeros(width, dtype=np.float32)
    output = rng.normal(0, math.sqrt(1 / width), width).astype(np.float32)
    params = (weights, bias, output)
    first = [np.zeros_like(p) for p in params]
    second = [np.zeros_like(p) for p in params]
    step = 0

    for epoch in range(epochs):
        order = rng.permutation(len(positions))
        loss = 0
        rate = 0.00045 + 0.0017 * (1 + math.cos(math.pi * epoch / epochs)) / 2
        for offset in range(0, len(order), 96):
            batch = [positions[i] for i in order[offset:offset + 96]]
            lengths = np.array([len(features) for _, features in batch])
            starts = np.cumsum(lengths) - lengths
            targets = starts + [target for target, _ in batch]
            features = np.concatenate([features for _, features in batch])
            states = np.repeat(np.arange(len(batch)), lengths)
            hidden = np.maximum(features @ weights + bias, 0)
            logits = hidden @ output
            # Each position has a different number of legal candidates.
            exp = np.exp(logits - np.maximum.reduceat(logits, starts)[states])
            probs = exp / np.add.reduceat(exp, starts)[states]
            loss -= np.log(np.maximum(probs[targets], 1e-12)).sum()

            probs[targets] -= 1
            probs /= len(batch)
            hidden_grad = probs[:, None] * output * (hidden > 0)
            gradients = (features.T @ hidden_grad, hidden_grad.sum(axis=0), hidden.T @ probs)
            step += 1
            for param, grad, m, v in zip(params, gradients, first, second):
                m *= 0.9
                m += 0.1 * grad
                v *= 0.999
                v += 0.001 * grad**2
                param -= rate * (m / (1 - 0.9**step)) / (np.sqrt(v / (1 - 0.999**step)) + 1e-8)
        if (epoch + 1) % 5 == 0 or epoch + 1 == epochs:
            print(f"epoch {epoch + 1}: train loss {loss / len(positions):.4f}", flush=True)

    weights = weights / scale[:, None]
    return {"hidden": np.round(weights.T.astype(np.float64), 6).tolist(),
            "bias": np.round((bias - mean @ weights).astype(np.float64), 6).tolist(),
            "output": np.round(output.astype(np.float64), 6).tolist()}


def position_key(features):
    # A rotation can reorder candidates while giving the network identical inputs.
    canonical = sorted(row.tobytes() for row in features.astype("<f4", copy=False))
    return hashlib.sha256(b"".join(canonical)).digest()


def evaluate(rows, model, games, training):
    weights = np.array(model["hidden"]).T
    bias = np.array(model["bias"])
    output = np.array(model["output"])
    seen = {position_key(row[3]) for row in training}
    eligible = Counter()
    groups = defaultdict(list)
    for _, size, target, features in rows:
        eligible[size] += 1
        eligible["all"] += 1
        if position_key(features) in seen:
            continue
        logits = np.maximum(features @ weights + bias, 0) @ output
        rank = int(np.where(np.argsort(-logits, kind="stable") == target)[0][0])
        result = (rank == 0, target == 0, 1 / len(features),
                  rank < 5, target < 5, min(5, len(features)) / len(features))
        groups[size].append(result)
        groups["all"].append(result)
    for size, group in groups.items():
        selected = [g for g in games if size == "all" or size == g["size"]]
        moves = Counter(m for g in selected for m in g["moves"][g["opening"]:])
        total = sum(moves.values()) - moves["pass"]
        rates = np.mean(group, axis=0) * 100
        print(f"{size}: {len(selected)} held-out games, {eligible[size]}/{total} point moves in candidate set, "
              f"{moves['pass']} passes, {len(group)} unseen positions; top-1 neural/old/uniform {rates[0]:.1f}/{rates[1]:.1f}/{rates[2]:.1f}%; "
              f"top-5 {rates[3]:.1f}/{rates[4]:.1f}/{rates[5]:.1f}%")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--games", type=Path, required=True)
    parser.add_argument("--examples", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--epochs", type=int, default=30)
    args = parser.parse_args()
    games = [json.loads(line) for line in args.games.read_text().splitlines()]
    holdout = {g["game"] for g in games[len(games) * 5 // 6:]}
    rows = load(args.examples)
    training = [row for row in rows if row[0] not in holdout]
    testing = [row for row in rows if row[0] in holdout]
    if not training or not testing:
        raise ValueError("training and holdout must both contain positions")
    print(f"Fitting {len(training)} positions, {len(training[0][3][0])} inputs", flush=True)
    model = train(training, args.epochs, seed=3817)
    args.output.write_text(json.dumps(model, separators=(",", ":")) + "\n")
    evaluate(testing, model, [g for g in games if g["game"] in holdout], training)


if __name__ == "__main__":
    main()
