import assert from "node:assert/strict";
import test from "node:test";

let moduleID = 0;
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
async function harness(t, { mode = "wasm", cores = 8, memory, workerThrows = false } = {}) {
  const names = ["document", "Worker", "setTimeout", "clearTimeout", "fetch", "navigator", "performance"];
  const originals = Object.fromEntries(names.map(name => [name, Object.getOwnPropertyDescriptor(globalThis, name)]));
  t.after(() => {
    for (const name of names) {
      if (originals[name]) Object.defineProperty(globalThis, name, originals[name]);
      else delete globalThis[name];
    }
  });
  const timers = new Map(), workers = [];
  let timerID = 0, now = 0, shouldThrow = workerThrows;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { hardwareConcurrency: cores, deviceMemory: memory } });
  Object.defineProperty(globalThis, "performance", { configurable: true, value: { now: () => now } });
  globalThis.document = { documentElement: { dataset: { engine: mode } } };
  globalThis.setTimeout = callback => { timers.set(++timerID, callback); return timerID; };
  globalThis.clearTimeout = id => timers.delete(id);
  globalThis.Worker = class {
    sent = [];
    stopped = false;
    constructor(url) { if (shouldThrow) throw Error("unsupported"); this.url = url; workers.push(this); }
    postMessage(message) { this.sent.push(message); }
    terminate() { this.stopped = true; }
    emit(data) { this.onmessage({ data }); }
    reply(result, index = this.sent.length - 1) { this.emit({ id: this.sent[index].id, ...result }); }
  };
  const engine = await import(`../web/engine.js?test=${++moduleID}`);
  return { ...engine, timers, workers, setNow: value => { now = value; }, setWorkerThrows: value => { shouldThrow = value; } };
}

async function ready(engine) {
  const waiting = engine.initializeEngine();
  engine.workers[0].emit({ type: "ready" });
  await waiting;
}
async function catalogue(engine) {
  const waiting = engine.runGame({ action: "sync", includeOpponents: true });
  await flush();
  engine.workers[0].reply({ game: { opponents: [{ id: "alphago", search: { roots: 32 } }, { id: "china" }] } });
  await waiting;
}
const play = { size: 9, action: "play", style: "alphago", x: 4, y: 4, moves: [] };
const config = { roots: 32, maxDepth: 3, nodeBudget: 8192, thinkMS: 300 };
const analysis = (shard = 0, shards = 1, depth = 1, complete = true) => ({ shard, shards, depth, complete, move: { x: 2, y: 2 }, score: 10 });

async function startSearch(engine, override = {}, seed = analysis()) {
  const waiting = engine.runGame(play);
  await flush();
  assert.equal(engine.workers[0].sent.at(-1).operation, "analyze");
  engine.workers[0].reply({ analysis: seed, config: { ...config, ...override } });
  await flush();
  for (const worker of engine.workers.slice(1)) worker.emit({ type: "ready" });
  await flush();
  return { waiting };
}

test("one initialization pairs requests and rejects illegal moves without retry", async t => {
  const engine = await harness(t);
  const initializing = engine.initializeEngine();
  assert.equal(engine.initializeEngine(), initializing);
  assert.equal(engine.workers.length, 1);
  const worker = engine.workers[0];
  assert.ok(worker.url.pathname.endsWith("/web/engine-worker.js"));
  worker.emit({ type: "ready" });
  await initializing;
  const result = engine.runGame({ action: "sync" });
  await flush();
  assert.deepEqual(worker.sent[0].payload, { action: "sync" });
  worker.reply({ game: { size: 9 } });
  assert.deepEqual(await result, { size: 9 });
  const rejected = assert.rejects(engine.runGame({ action: "play" }), /不能落子/);
  await flush();
  worker.reply({ error: "不能落子" });
  await rejected;
  assert.equal(engine.workers.length, 1);
  assert.equal(engine.timers.size, 0);
});

test("search lazily starts a bounded multicore pool and merges the deepest complete round", async t => {
  const engine = await harness(t, { cores: 16, memory: 8 });
  await ready(engine);
  await catalogue(engine);
  assert.equal(engine.workers.length, 1, "catalogue creates no spare Go heaps");
  const { waiting } = await startSearch(engine);
  assert.equal(engine.workers.length, 8, "browser cap is eight, even on a many-core host");
  for (let depth = 2; depth <= 3; depth++) {
    for (const [shard, worker] of engine.workers.entries()) {
      const sent = worker.sent.at(-1);
      assert.equal(sent.operation, "analyze");
      assert.equal(sent.payload.shard, shard);
      assert.equal(sent.payload.shards, 8);
      assert.equal(sent.payload.depth, depth);
      assert.equal(sent.payload.nodeBudget, 8192, "per-root budget is unchanged by sharding");
      assert.ok(sent.payload.thinkMS > 0 && sent.payload.thinkMS <= config.thinkMS, "Go receives an actual remaining-time budget");
      worker.reply({ analysis: analysis(shard, 8, depth), config });
    }
    await flush();
  }
  const primary = engine.workers[0];
  assert.equal(primary.sent.at(-1).operation, "merge");
  assert.equal(primary.sent.at(-1).payload.analyses.length, 8);
  assert.ok(primary.sent.at(-1).payload.analyses.every(result => result.depth === 3));
  primary.reply({ game: { size: 9, search: { workers: 8, depth: 3 } } });
  assert.deepEqual((await waiting).search, { workers: 8, depth: 3 });
  assert.equal(engine.timers.size, 0);
});

test("incomplete or time-limited deeper rounds retain the completed earlier result", async t => {
  const engine = await harness(t, { cores: 4 });
  await ready(engine);
  await catalogue(engine);
  const { waiting } = await startSearch(engine);
  assert.equal(engine.workers.length, 3, "one logical core is reserved for the UI");
  for (const [shard, worker] of engine.workers.entries()) worker.reply({ analysis: analysis(shard, 3, 2, shard !== 1), config });
  engine.setNow(400);
  await flush();
  assert.deepEqual(engine.workers[0].sent.at(-1).payload.analyses, [analysis()]);
  engine.workers[0].reply({ game: { search: { workers: 1, depth: 1 } } });
  assert.equal((await waiting).search.depth, 1);
  assert.equal(engine.workers.some(worker => worker.stopped), false, "normal deadline exhaustion does not restart workers");
  assert.equal(engine.timers.size, 0);
});

test("an incomplete first search falls back for this move without retiring the healthy worker", async t => {
  const engine = await harness(t, { cores: 4 });
  await ready(engine);
  await catalogue(engine);
  const primary = engine.workers[0];
  const first = engine.runGame(play);
  await flush();
  primary.reply({ analysis: analysis(0, 1, 1, false), config });
  await flush();
  assert.equal(engine.workers.length, 1, "an incomplete seed must not allocate idle helpers");
  assert.equal(primary.stopped, false);
  assert.equal(primary.sent.at(-1).operation, "run");
  primary.reply({ game: { size: 9 } });
  assert.equal((await first).size, 9);
  const { waiting } = await startSearch(engine, { maxDepth: 2 });
  assert.equal(engine.workers.length, 3, "a later move can still use parallel search");
  for (const [shard, worker] of engine.workers.entries()) worker.reply({ analysis: analysis(shard, 3, 2), config });
  await flush();
  primary.reply({ game: { size: 9, search: { workers: 3, depth: 2 } } });
  assert.equal((await waiting).search.workers, 3);
  assert.equal(engine.timers.size, 0);
});

test("a seed that exhausts the time budget merges directly without starting idle helpers", async t => {
  const engine = await harness(t, { cores: 8 });
  await ready(engine);
  await catalogue(engine);
  const waiting = engine.runGame(play);
  await flush();
  engine.setNow(config.thinkMS + 1);
  engine.workers[0].reply({ analysis: analysis(), config });
  await flush();
  assert.equal(engine.workers.length, 1);
  assert.equal(engine.workers[0].sent.at(-1).operation, "merge");
  assert.deepEqual(engine.workers[0].sent.at(-1).payload.analyses, [analysis()]);
  engine.workers[0].reply({ game: { search: { workers: 1, depth: 1 } } });
  await waiting;
  assert.equal(engine.timers.size, 0);
});

test("low memory and single-core browsers retain a single worker", async t => {
  for (const options of [{ cores: 1 }, { cores: 64, memory: 2 }]) {
    const engine = await harness(t, options);
    await ready(engine);
    await catalogue(engine);
    const waiting = engine.runGame(play);
    await flush();
    assert.equal(engine.workers.length, 1);
    assert.equal(engine.workers[0].sent.at(-1).operation, "run");
    engine.workers[0].reply({ game: { size: 9 } });
    await waiting;
  }
});

test("reported memory and candidate roots reduce the lazy pool size", async t => {
  const engine = await harness(t, { cores: 64, memory: 4 });
  await ready(engine);
  await catalogue(engine);
  const { waiting } = await startSearch(engine, { roots: 3, maxDepth: 2 });
  assert.equal(engine.workers.length, 2);
  for (const [shard, worker] of engine.workers.entries()) worker.reply({ analysis: analysis(shard, 2, 2), config });
  await flush();
  engine.workers[0].reply({ game: { size: 9 } });
  await waiting;
  assert.equal(engine.timers.size, 0);
});

test("a nearly finished board starts no more workers than actual root candidates", async t => {
  const engine = await harness(t, { cores: 64, memory: 8 });
  await ready(engine);
  await catalogue(engine);
  const { waiting } = await startSearch(engine, { maxDepth: 2 }, { ...analysis(), totalRoots: 2 });
  assert.equal(engine.workers.length, 2);
  for (const [shard, worker] of engine.workers.entries()) worker.reply({ analysis: analysis(shard, 2, 2), config });
  await flush();
  engine.workers[0].reply({ game: { size: 9 } });
  await waiting;
});

test("pool crashes clean up every worker and retry this complete history once on a fresh single worker", async t => {
  const engine = await harness(t, { cores: 4 });
  await ready(engine);
  await catalogue(engine);
  const { waiting } = await startSearch(engine);
  const previous = [...engine.workers];
  engine.workers[1].onerror();
  await flush();
  assert.ok(previous.every(worker => worker.stopped));
  const fallback = engine.workers.at(-1);
  assert.notEqual(fallback, previous[0]);
  fallback.emit({ type: "ready" });
  await flush();
  assert.equal(fallback.sent[0].operation, "run");
  assert.deepEqual(fallback.sent[0].payload, play);
  fallback.reply({ game: { size: 9 } });
  assert.equal((await waiting).size, 9);
  const another = engine.runGame(play);
  await flush();
  assert.equal(fallback.sent.at(-1).operation, "run", "parallel mode stays disabled for this page after failure");
  fallback.reply({ game: { size: 9 } });
  await another;
  assert.equal(engine.workers.length, previous.length + 1);
  assert.equal(engine.timers.size, 0);
});

test("a transport timeout falls back once and a failed retry rejects without leaking timers", async t => {
  const engine = await harness(t);
  await ready(engine);
  const waiting = engine.runGame({ action: "sync" });
  const rejected = assert.rejects(waiting, /超时/);
  await flush();
  engine.timers.values().next().value();
  await flush();
  assert.equal(engine.workers[0].stopped, true);
  const fallback = engine.workers[1];
  fallback.emit({ type: "ready" });
  await flush();
  engine.timers.values().next().value();
  await rejected;
  assert.equal(fallback.stopped, true);
  assert.equal(engine.workers.length, 2);
  assert.equal(engine.timers.size, 0);
});

test("failed replacement creation rejects this call but permits the next explicit retry", async t => {
  const engine = await harness(t);
  await ready(engine);
  const failed = assert.rejects(engine.runGame({ action: "sync" }), /无法启动/);
  await flush();
  engine.setWorkerThrows(true);
  engine.workers[0].onerror();
  await failed;
  assert.equal(engine.workers.length, 1, "this call makes only its one bounded replacement attempt");
  assert.equal(engine.workers[0].stopped, true);
  assert.equal(engine.timers.size, 0);
  engine.setWorkerThrows(false);
  const retry = engine.runGame({ action: "sync" });
  assert.equal(engine.workers.length, 2, "an explicit retry creates a new primary after replacement failure");
  engine.workers[1].emit({ type: "ready" });
  await flush();
  assert.equal(engine.workers[1].sent[0].operation, "run");
  engine.workers[1].reply({ game: { size: 9 } });
  assert.equal((await retry).size, 9);
  assert.equal(engine.timers.size, 0);
});

test("startup failure rejects readiness without keeping timers", async t => {
  const engine = await harness(t);
  const failed = assert.rejects(engine.initializeEngine(), /加载/);
  engine.workers[0].emit({ type: "initerror" });
  await failed;
  assert.equal(engine.workers[0].stopped, true);
  assert.equal(engine.timers.size, 0);
});

test("HTTP mode keeps its relative API transport and clears timeouts", async t => {
  const engine = await harness(t, { mode: "http" });
  await engine.initializeEngine();
  assert.equal(engine.workers.length, 0);
  globalThis.fetch = async (url, options) => {
    assert.equal(url, "./api/game");
    assert.equal(options.method, "POST");
    assert.deepEqual(JSON.parse(options.body), { action: "sync" });
    return { ok: true, json: async () => ({ size: 9 }) };
  };
  assert.deepEqual(await engine.runGame({ action: "sync" }), { size: 9 });
  globalThis.fetch = async () => ({ ok: false, json: async () => ({ error: "坐标无效" }) });
  await assert.rejects(engine.runGame({}), /坐标无效/);
  globalThis.fetch = async () => { throw new TypeError("offline"); };
  await assert.rejects(engine.runGame({}), /无法连接/);
  assert.equal(engine.timers.size, 0);
});
