const timeoutMS = 15000;
let primary;
let helpers = [];
let poolInitialization;
let recovery;
let parallelDisabled = false;
let sequence = 0;
const searchOpponents = new Set();

function workerError(message) {
  return Object.assign(new Error(message), { workerFailure: true });
}

function workerLimit() {
  const cores = Math.max(1, Math.floor(Number(globalThis.navigator?.hardwareConcurrency) || 1));
  const memory = Number(globalThis.navigator?.deviceMemory);
  // Chromium clamps deviceMemory at 8 GiB, even on larger desktops.
  const memoryLimit = !Number.isFinite(memory) || memory <= 0 || memory >= 8 ? 8 : memory <= 2 ? 1 : memory <= 4 ? 2 : 4;
  return Math.min(8, Math.max(1, cores - 1), memoryLimit);
}

function createWorker() {
  const pending = new Map();
  let worker, failure, startupTimer, resolveReady, rejectReady;
  const ready = new Promise((resolve, reject) => { resolveReady = resolve; rejectReady = reject; });
  function stop(error) {
    if (failure) return;
    failure = error;
    clearTimeout(startupTimer);
    worker?.terminate();
    rejectReady(error);
    for (const request of pending.values()) {
      clearTimeout(request.timeout);
      request.reject(error);
    }
    pending.clear();
  }
  try {
    worker = new Worker(new URL("./engine-worker.js", import.meta.url));
    worker.onmessage = ({ data }) => {
      if (failure) return;
      if (data.type === "ready") {
        clearTimeout(startupTimer);
        resolveReady();
      } else if (data.type === "initerror" || data.type === "workererror") {
        stop(workerError("棋局引擎加载或计算失败，请重试。当前棋局已保留。"));
      } else {
        const request = pending.get(data.id);
        if (!request) return;
        clearTimeout(request.timeout);
        pending.delete(data.id);
        if (data.error) request.reject(new Error(data.error));
        else request.resolve(data);
      }
    };
    worker.onerror = () => stop(workerError("棋局引擎已停止，请重试。当前棋局已保留。"));
    worker.onmessageerror = () => stop(workerError("棋局数据读取失败，请重试。"));
    startupTimer = setTimeout(() => stop(workerError("棋局引擎加载超时，请重试。")), timeoutMS);
  } catch {
    stop(workerError("浏览器无法启动棋局引擎，请更新浏览器后重试。"));
  }
  return {
    ready,
    stop,
    get failed() { return !!failure; },
    async request(operation, payload) {
      await ready;
      if (failure) throw failure;
      return new Promise((resolve, reject) => {
        const id = ++sequence;
        const timeout = setTimeout(() => stop(workerError("等待棋局超时，请重试。当前棋局已保留。")), timeoutMS);
        pending.set(id, { resolve, reject, timeout });
        try { worker.postMessage({ id, operation, payload }); }
        catch { stop(workerError("无法提交棋局，请重试。当前棋局已保留。")); }
      });
    },
  };
}

export function initializeEngine() {
  if (document.documentElement.dataset.engine !== "wasm") return Promise.resolve();
  if (!primary || primary.failed) primary = createWorker();
  return primary.ready;
}

async function searchPool(count) {
  // Extra Go heaps are allocated only when an advanced opponent actually plays.
  if (!poolInitialization || helpers.length < count - 1) {
    while (helpers.length < count - 1) helpers.push(createWorker());
    poolInitialization = Promise.all(helpers.map(worker => worker.ready));
  }
  await poolInitialization;
  return [primary, ...helpers].slice(0, count);
}

async function parallelSearch(payload) {
  // One complete shallow result remains usable if a deeper round runs out of nodes.
  const started = performance.now();
  const seed = await primary.request("analyze", { request: payload, shard: 0, shards: 1, depth: 1 });
  if (seed.game) return seed.game;
  if (!seed.analysis || !seed.config) throw workerError("搜索数据不完整，请重试。");
  if (!seed.analysis.complete) return (await primary.request("run", payload)).game;
  const config = seed.config;
  const spent = performance.now() - started;
  const count = Math.min(workerLimit(), config.roots, seed.analysis.totalRoots ?? config.roots);
  if (count < 2) return (await primary.request("run", payload)).game;
  let analyses = [seed.analysis];
  if (config.maxDepth > 1 && spent < config.thinkMS) {
    const pool = await searchPool(count);
    // Worker startup is a one-time load, not search time; the shallow search counts.
    const deadline = performance.now() + config.thinkMS - spent;
    for (let depth = 2; depth <= config.maxDepth && performance.now() < deadline; depth++) {
      const thinkMS = Math.max(1, Math.floor(deadline - performance.now()));
      // The Go budget is per root, so partitioning roots must not divide it again.
      const round = await Promise.all(pool.map((worker, shard) => worker.request("analyze", {
        request: payload, shard, shards: pool.length, depth, nodeBudget: config.nodeBudget, thinkMS,
      })));
      if (round.some(result => !result.analysis?.complete)) break;
      analyses = round.map(result => result.analysis);
    }
  }
  return (await primary.request("merge", { request: payload, analyses })).game;
}

async function recoverSingleWorker() {
  if (!recovery) {
    recovery = (async () => {
      parallelDisabled = true;
      const error = workerError("并行计算已停止，正在恢复单线程棋局。");
      primary?.stop(error);
      for (const worker of helpers) worker.stop(error);
      helpers = [];
      poolInitialization = null;
      primary = createWorker();
      await primary.ready;
    })().finally(() => { recovery = null; });
  }
  return recovery;
}

export async function runGame(payload) {
  if (document.documentElement.dataset.engine === "wasm") {
    await initializeEngine();
    let game;
    try {
      const search = !parallelDisabled && workerLimit() > 1 && searchOpponents.has(payload.style) && (payload.action === "play" || payload.action === "pass");
      game = search ? await parallelSearch(payload) : (await primary.request("run", payload)).game;
    } catch (error) {
      if (!error.workerFailure) throw error;
      // Requests contain the entire history; retrying on a fresh worker is safe.
      await recoverSingleWorker();
      game = (await primary.request("run", payload)).game;
    }
    if (game?.opponents) {
      searchOpponents.clear();
      for (const opponent of game.opponents) if (opponent.search) searchOpponents.add(opponent.id);
    }
    return game;
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMS);
  try {
    const response = await fetch("./api/game", {
      method: "POST", signal: controller.signal,
      headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || "这一步没有成功，请重试。");
    return data;
  } catch (error) {
    if (error.name === "AbortError") throw new Error("等待棋局超时，请重试。当前棋局已保留。");
    if (error instanceof TypeError || error instanceof SyntaxError) throw new Error("暂时无法连接棋局，请确认服务正在运行后重试。");
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}
