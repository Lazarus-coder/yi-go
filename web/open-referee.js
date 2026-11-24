import { inspectExhibition } from "./engine.js";

// Each request is self-contained, so independent games may share one WASM worker.
export async function createReferee(maxMoves = 80) {
  const request = (history, chooseCPU = false) => inspectExhibition({ history, maxMoves, chooseCPU });
  let state = await request([]);
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
    async cpu() {
      if (state.result) throw new Error("本局已结束");
      return (await request(state.turn.history, true)).choice;
    },
  };
}
