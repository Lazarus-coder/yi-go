// Run after build-pages.sh; no browser, npm packages, or HTTP server needed.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("../dist/play.html", import.meta.url), "utf8");
assert.match(html, /data-engine="wasm"/);
assert.match(html, /type="module" src="\.\/app\.js"/);
assert.doesNotMatch(html, /(?:src|href)="\/[^/]/, "Pages assets must support a project subpath");
await import("../dist/wasm_exec.js");
const go = new Go();
const ready = new Promise((resolve) => { globalThis.yiReady = resolve; });
const bytes = await readFile(new URL("../dist/game.wasm", import.meta.url));
const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
const running = go.run(instance);
await Promise.race([ready, running.then(() => { throw new Error("Go exited before readiness"); })]);
const run = (input) => JSON.parse(globalThis.yiRun(JSON.stringify(input)));

const catalogue = run({ size: 9, action: "sync", includeOpponents: true }).game.opponents;
assert.ok(catalogue.length > 3);
for (const { id: style } of catalogue) {
  const input = { size: 9, moves: [], style };
  const initial = run({ ...input, action: "sync" });
  assert.equal(initial.game.style, style);
  assert.equal(initial.game.opponents, undefined);
  const played = run({ ...input, action: "play", x: 4, y: 4 });
  assert.equal(played.error, undefined);
  assert.equal(played.game.moves.length, 2, `${style} CPU must reply`);
  assert.equal(played.game.board[40], 1);
  const restored = run({ ...input, action: "sync", moves: played.game.moves });
  assert.deepEqual(restored.game.board, played.game.board);
  assert.equal(run({ ...input, action: "resign" }).game.resigned, 1);
}
assert.equal(run({ size: 13, moves: [], action: "sync" }).game.board.length, 169);
assert.equal(run({ size: 19, moves: [], action: "sync" }).game.board.length, 361);
// Match the native policy on the same 9×9 opening and search depth.
const opening = { size: 9, style: "alphago", moves: [], action: "play", x: 4, y: 4 };
const { analysis } = JSON.parse(globalThis.yiAnalyze(JSON.stringify({ request: opening, depth: 1 })));
assert.ok(analysis.complete);
assert.deepEqual(analysis.move, { x: 4, y: 2 }, "neural reply to E5 at depth one is E7");
assert.ok(run({ size: 9, action: "sync", style: "unknown" }).error);
assert.ok(run({ size: 9, action: "sync", extra: true }).error);
assert.ok(JSON.parse(globalThis.yiRun("{} {}")).error);
assert.ok(JSON.parse(globalThis.yiRun(" ".repeat(65537))).error);
assert.ok(JSON.parse(globalThis.yiRun(null)).error);
console.log(`WASM smoke check passed for all ${catalogue.length} selectable opponents, replay, resign, sizes and input validation.`);

const inspect = (history, maxMoves = 80, chooseCPU = false) =>
  JSON.parse(globalThis.yiExhibition(JSON.stringify({ history, maxMoves, chooseCPU })));
const first = inspect([], 80, true);
assert.equal(first.turn.color, "black");
assert.equal(first.turn.board.length, 81);
assert.equal(first.turn.legal.length, 83);
assert.ok(first.turn.legal.includes(first.choice));
const second = inspect([first.choice], 80, true);
assert.equal(second.turn.color, "white");
assert.ok(second.turn.legal.includes(second.choice));
assert.deepEqual(second.turn.history, [first.choice]);
assert.ok(inspect([first.choice, first.choice]).error, "an occupied intersection is illegal");
const passed = inspect(["pass", "pass"]);
assert.equal(passed.result.reason, "passes");
assert.equal(passed.result.score.white, 6.5);
assert.equal(passed.result.winner, "white");
assert.equal(passed.turn.legal.length, 0);
assert.deepEqual(inspect(["D4","resign"]).result, { reason: "resign", winner: "black" });
assert.equal(inspect(["D4","E5"], 2).result.reason, "move-cap");
assert.ok(inspect(["pass", "pass", "A1"]).error);
assert.ok(inspect([],145).error);
console.log("Public exhibition WASM referee passed two-seat play, CPU, passes, resignation, move cap and illegal replay checks.");
