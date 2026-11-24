// Run after build-pages.sh and build-html.mjs. No npm packages are needed.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { Worker } from "node:worker_threads";
import test from "node:test";

const html = await readFile(new URL("../dist/yi-go.html", import.meta.url), "utf8");
const assetTag = html.match(/<script id="yi-offline-assets" type="application\/json">([\s\S]*?)<\/script>/);
assert.ok(assetTag, "offline asset payload is embedded");
const assets = JSON.parse(assetTag[1]);

test("play and watch entries include all modules, styles and images", async () => {
  assert.equal(assets.play.entry, "app.js");
  assert.equal(assets.watch.entry, "watch.js");
  for (const [name, module] of Object.entries(assets.modules)) {
    for (const dependency of Object.values(module.imports)) assert.ok(assets.modules[dependency], `${name} needs ${dependency}`);
  }
  assert.match(html, /data-engine="wasm"/);
  assert.match(html, /data-standalone="true"/);
  assert.match(assets.modules["watch.js"].source, /dataset\.standalone/);
  assert.match(html, /data:image\/jpeg;base64,/);
  assert.doesNotMatch(html.replace(assetTag[0], ""), /<(?:script|link)\b[^>]*(?:src|href)=["']\.\//);
  assert.match(assets.watch.body, /href="#play"/);
  assert.doesNotMatch(assets.watch.body, /href=["']\.\//);
  const bootstrap = html.match(/<script type="module">([\s\S]*?)<\/script>/)?.[1];
  assert.ok(bootstrap, "offline module loader is embedded");
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  assert.doesNotThrow(() => new AsyncFunction(bootstrap), "generated loader is valid JavaScript");
  const { exhibition } = await import("data:text/javascript;base64," + Buffer.from(assets.modules["exhibition.js"].source).toString("base64"));
  assert.ok(exhibition.moves.length > 10, "offline replay contains a real game");
  assert.equal(exhibition.frames.length, exhibition.moves.length + 1);
  for (const frame of exhibition.frames) assert.equal(frame.board.length, exhibition.size ** 2);
});

test("embedded Go worker plays offline with all network requests forbidden", { timeout: 15000 }, async (t) => {
  const worker = new Worker(`
    const { parentPort, workerData } = require("node:worker_threads");
    const vm = require("node:vm");
    globalThis.self = globalThis;
    globalThis.postMessage = data => parentPort.postMessage(data);
    parentPort.on("message", data => self.onmessage({ data }));
    globalThis.importScripts = url => {
      if (!url.startsWith("data:text/javascript;base64,")) throw Error("External script forbidden");
      vm.runInThisContext(Buffer.from(url.split(",")[1], "base64").toString());
    };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = url => {
      if (!url.startsWith("data:")) throw Error("Network forbidden: " + url);
      return originalFetch(url);
    };
    vm.runInThisContext(workerData);
  `, { eval: true, workerData: assets.worker });
  t.after(() => worker.terminate());
  const nextMessage = () => new Promise((resolve, reject) => {
    const fail = error => { worker.off("message", message); reject(error); };
    const message = data => { worker.off("error", fail); resolve(data); };
    worker.once("message", message);
    worker.once("error", fail);
  });
  assert.deepEqual(await nextMessage(), { type: "ready" });
  let response = nextMessage();
  worker.postMessage({ id: 1, payload: { size: 9, style: "alphago", moves: [], action: "play", x: 4, y: 4 } });
  const played = await response;
  assert.equal(played.error, undefined);
  assert.equal(played.game.moves.length, 2);
  assert.equal(played.game.board[40], 1);
  response = nextMessage();
  worker.postMessage({ id: 2, payload: { size: 9, style: "alphago", moves: played.game.moves, action: "sync" } });
  assert.deepEqual((await response).game.board, played.game.board);
});
