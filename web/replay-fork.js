const columns = "ABCDEFGHJKLMNOPQRST";

export function forkLink(size, moves) {
  const coordinates = moves.map(move => move.x === -1 ? "pass" : columns[move.x] + (size - move.y));
  return "./join.html#fork=" + size + "&moves=" + coordinates.join(",");
}

// The Go referee replays this history to check captures, suicide and ko.
export function readFork(hash) {
  const params = new URLSearchParams(hash.replace(/^#/, ""));
  if (!params.has("fork")) return null;
  const size = Number(params.get("fork"));
  const raw = params.get("moves");
  if (![9, 13, 19].includes(size) || raw === null || raw.length > 10000 ||
      params.getAll("fork").length !== 1 || params.getAll("moves").length !== 1) {
    throw new Error("续弈链接无效，请从棋谱重新选择局面。");
  }
  const moves = raw === "" ? [] : raw.split(",");
  if (moves.length > 2000 || moves.some(move => move !== "pass" &&
      (!/^[A-HJ-T][1-9][0-9]?$/.test(move) || columns.indexOf(move[0]) >= size || Number(move.slice(1)) > size))) {
    throw new Error("续弈链接中的棋谱无效。");
  }
  return { size, moves };
}
