// Run after build-pages.sh; no browser, npm packages, or HTTP server needed.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("../dist/index.html", import.meta.url), "utf8");
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

for (const style of ["china", "japan", "korea"]) {
  const input = { size: 9, moves: [], style };
  const initial = run({ ...input, action: "sync" });
  assert.equal(initial.game.style, style);
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
assert.ok(run({ size: 9, action: "sync", style: "unknown" }).error);
assert.ok(run({ size: 9, action: "sync", extra: true }).error);
assert.ok(JSON.parse(globalThis.yiRun("{} {}")).error);
assert.ok(JSON.parse(globalThis.yiRun(" ".repeat(65537))).error);
assert.ok(JSON.parse(globalThis.yiRun(null)).error);
console.log("WASM smoke check passed: three CPU styles, replay, resign, sizes, input validation.");
