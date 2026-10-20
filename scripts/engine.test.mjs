import assert from "node:assert/strict";
import test from "node:test";

let moduleID = 0;
async function harness(t, mode = "wasm") {
  const names = ["document", "Worker", "setTimeout", "clearTimeout", "fetch"];
  const originals = Object.fromEntries(names.map((name) => [name, globalThis[name]]));
  t.after(() => {
    for (const name of names) {
      if (originals[name] === undefined) delete globalThis[name];
      else globalThis[name] = originals[name];
    }
  });
  const timers = new Map();
  const workers = [];
  let timerID = 0;
  globalThis.document = { documentElement: { dataset: { engine: mode } } };
  globalThis.setTimeout = (callback) => { timers.set(++timerID, callback); return timerID; };
  globalThis.clearTimeout = (id) => timers.delete(id);
  globalThis.Worker = class {
    sent = [];
    stopped = false;
    constructor(url) { this.url = url; workers.push(this); }
    postMessage(message) { this.sent.push(message); }
    terminate() { this.stopped = true; }
    emit(data) { this.onmessage({ data }); }
  };
  const engine = await import(`../web/engine.js?test=${++moduleID}`);
  return { ...engine, timers, workers };
}

test("one initialization waits for readiness and pairs responses with requests", async (t) => {
  const engine = await harness(t);
  const ready = engine.initializeEngine();
  assert.equal(engine.initializeEngine(), ready);
  assert.equal(engine.workers.length, 1);
  const worker = engine.workers[0];
  assert.equal(worker.url.pathname.endsWith("/web/engine-worker.js"), true);
  worker.emit({ type: "ready" });
  await ready;
  assert.equal(engine.timers.size, 0);
  const result = engine.runGame({ action: "sync" });
  await Promise.resolve();
  const request = worker.sent[0];
  assert.deepEqual(request.payload, { action: "sync" });
  worker.emit({ id: request.id, game: { size: 9 } });
  assert.deepEqual(await result, { size: 9 });
  const illegal = engine.runGame({ action: "play" });
  const rejected = assert.rejects(illegal, /不能落子/);
  await Promise.resolve();
  worker.emit({ id: worker.sent[1].id, error: "不能落子" });
  await rejected;
  assert.equal(engine.timers.size, 0);
});

test("a timed-out worker rejects every request and releases every timer", async (t) => {
  const engine = await harness(t);
  const ready = engine.initializeEngine();
  const worker = engine.workers[0];
  worker.emit({ type: "ready" });
  await ready;
  const first = assert.rejects(engine.runGame({ action: "sync" }), /超时/);
  const second = assert.rejects(engine.runGame({ action: "sync" }), /超时/);
  await Promise.resolve();
  engine.timers.values().next().value();
  await Promise.all([first, second]);
  assert.equal(worker.stopped, true);
  assert.equal(engine.timers.size, 0);
  await assert.rejects(engine.runGame({ action: "sync" }), /超时/);
});

test("a startup failure rejects readiness without leaving timers", async (t) => {
  const engine = await harness(t);
  const failed = assert.rejects(engine.initializeEngine(), /加载失败/);
  engine.workers[0].emit({ type: "initerror" });
  await failed;
  assert.equal(engine.workers[0].stopped, true);
  assert.equal(engine.timers.size, 0);
});

test("HTTP mode sends a relative request and releases its timeout on errors", async (t) => {
  const engine = await harness(t, "http");
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
