import { inspectExhibition, runGame } from "./engine.js";

// Each request is self-contained, so independent games may share one WASM worker.
export async function createReferee(maxMoves = 80, { size = 9, history = [], startPly = 0 } = {}) {
  const request = moves => inspectExhibition({ history: moves, size, maxMoves, startPly });
  let state = await request(history);
  return {
    turn: () => state.turn,
    snapshot: () => state.game,
    result: () => state.result,
    async play(move) {
      if (state.result) throw new Error("本局已结束");
      const previous = state;
      const next = await request([...previous.turn.history, move]);
      if (state !== previous) throw new Error("这手棋已经提交。");
      state = next;
    },
    async cpu(style = "club-fighter") {
      if (state.result) throw new Error("本局已结束");
      const reply = await runGame({ size, moves: state.game.moves, style, action: "cpu" });
      const point = reply.lastMove;
      return point ? "ABCDEFGHJKLMNOPQRST"[point.x] + (size - point.y) : "pass";
    },
  };
}
