// Store only accepted moves and public seat settings. Provider keys stay in the player closure.
const prefix = "yi-exhibition-v1:";
export const latestExhibition = prefix + "latest";
export const isGameID = id => typeof id === "string" && /^[0-9a-f]{32}$/.test(id);
export const saveKey = id => prefix + id;
export const cpuNames = {"club-fighter":"棋社 · 战斗派", alphago:"神经棋手"};

export function encodeGame(record) {
  const {gameId, size, startPly, maxMoves, moves, pending} = record;
  const seats = {};
  const calls = {};
  for (const color of ["black", "white"]) {
    const seat = record.seats[color];
    seats[color] = seat.kind === "cpu" ? {kind:"cpu", style:seat.style} :
      {kind:seat.kind, endpoint:seat.endpoint, needsKey:seat.needsKey, ...(seat.kind === "chat" ? {model:seat.model} : {})};
    calls[color] = record.calls[color];
  }
  return JSON.stringify({gameId, size, startPly, maxMoves, moves, calls, pending, seats});
}

export function decodeGame(raw, id) {
  const invalid = () => { throw new Error("存档无法读取，可另开新局。"); };
  if (!raw || raw.length > 65536) invalid();
  let r;
  try { r = JSON.parse(raw); } catch { invalid(); }
  if (!r || !isGameID(id) || r.gameId !== id || ![9,13,19].includes(r.size) ||
      !Number.isInteger(r.startPly) || r.startPly < 0 || r.startPly > 2000 ||
      !Number.isInteger(r.maxMoves) || r.maxMoves-r.startPly < 2 || r.maxMoves-r.startPly > 144 ||
      !Array.isArray(r.moves) || r.moves.length < r.startPly || r.moves.length > r.maxMoves ||
      !r.moves.every(m => typeof m === "string" && /^(?:[A-HJ-T][1-9][0-9]?|pass|resign)$/.test(m)) ||
      ![null, "black", "white"].includes(r.pending)) invalid();
  const kinds = {};
  for (const color of ["black","white"]) {
    const seat = r.seats?.[color];
    if (!seat || !["cpu","agent","chat"].includes(seat.kind) ||
        !Number.isSafeInteger(r.calls?.[color]) || r.calls[color] < 0) invalid();
    if (seat.kind === "cpu" && !Object.hasOwn(cpuNames, seat.style)) invalid();
    if (seat.kind !== "cpu" && (typeof seat.endpoint !== "string" || seat.endpoint.length > 2048 ||
        typeof seat.needsKey !== "boolean")) invalid();
    if (seat.kind === "chat" && (typeof seat.model !== "string" || seat.model.length > 160)) invalid();
    kinds[color] = seat.kind;
  }
  return {...r, kinds, result:null};
}
