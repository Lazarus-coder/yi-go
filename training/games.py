"""Generate fresh GNU Go self-play positions, with independent seeds per game."""
import argparse
import json
import random
import subprocess
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

COLUMNS = "ABCDEFGHJKLMNOPQRST"


def teacher_game(index, teacher):
    size = (9, 9, 13, 9, 13, 19)[index % 6]
    rng = random.Random(20261124 + index)
    commands = [f"boardsize {size}", "komi 6.5", f"set_random_seed {rng.randrange(1, 2**30)}"]
    points = [(x, y) for x in range(1, size - 1) for y in range(1, size - 1)]
    opening = []
    for _ in range(1 + index % 7):
        x, y = rng.choice(points)
        move = COLUMNS[x] + str(y + 1)
        commands.append(f"play {'BW'[len(opening) % 2]} {move}")
        opening.append(move)
        points = [(u, v) for u, v in points if abs(x - u) + abs(y - v) > 1]
    cap = {9: 108, 13: 136, 19: 152}[size]
    start = len(commands)
    commands.extend(f"genmove {'BW'[ply % 2]}" for ply in range(len(opening), cap))
    result = subprocess.run([teacher, "--mode", "gtp", "--chinese-rules", "--level", "1", "--never-resign"],
                            input="\n".join(commands + ["quit"]) + "\n", text=True,
                            capture_output=True, check=True, timeout=240)
    replies = result.stdout.strip().split("\n\n")
    if len(replies) != len(commands) + 1 or any(not line.startswith("=") for line in replies):
        raise RuntimeError(f"GNU Go rejected a command in game {index}")
    moves = opening.copy()
    for reply in replies[start:-1]:
        move = reply[1:].strip()
        moves.append("pass" if move.lower() == "pass" else move.upper())
        if moves[-2:] == ["pass", "pass"]:
            break
    return dict(game=index, size=size, opening=len(opening), moves=moves)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gnugo", default="gnugo")
    parser.add_argument("--games", type=int, default=144)
    parser.add_argument("--jobs", type=int, default=4)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    with args.output.open("x") as out, ThreadPoolExecutor(args.jobs) as pool:
        for record in pool.map(lambda i: teacher_game(i, args.gnugo), range(args.games)):
            print(json.dumps(record, separators=(",", ":")), file=out, flush=True)
            if (record["game"] + 1) % 24 == 0:
                print(f"{record['game'] + 1}/{args.games} games", flush=True)


if __name__ == "__main__":
    main()
