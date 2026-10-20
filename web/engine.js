const timeoutMS = 15000;
const pending = new Map();
let worker;
let initialization;
let rejectInitialization;
let initializationTimeout;
let failure;
let sequence = 0;

function failWorker(message) {
  failure = new Error(message);
  clearTimeout(initializationTimeout);
  worker?.terminate();
  worker = null;
  rejectInitialization?.(failure);
  rejectInitialization = null;
  for (const request of pending.values()) {
    clearTimeout(request.timeout);
    request.reject(failure);
  }
  pending.clear();
}

export function initializeEngine() {
  if (document.documentElement.dataset.engine !== "wasm") return Promise.resolve();
  if (failure) return Promise.reject(failure);
  if (initialization) return initialization;
  initialization = new Promise((resolve, reject) => {
    rejectInitialization = reject;
    try {
      worker = new Worker(new URL("./engine-worker.js", import.meta.url));
      worker.onmessage = ({ data }) => {
        if (data.type === "ready") {
          clearTimeout(initializationTimeout);
          rejectInitialization = null;
          resolve();
          return;
        }
        if (data.type === "initerror") {
          failWorker("棋局引擎加载失败，请刷新后重试。");
          return;
        }
        const request = pending.get(data.id);
        if (!request) return;
        clearTimeout(request.timeout);
        pending.delete(data.id);
        if (data.error) request.reject(new Error(data.error));
        else request.resolve(data.game);
      };
      worker.onerror = () => failWorker("棋局引擎已停止，请刷新后重试。当前棋局已保留。");
      worker.onmessageerror = () => failWorker("棋局数据读取失败，请刷新后重试。");
      initializationTimeout = setTimeout(() => failWorker("棋局引擎加载超时，请刷新后重试。"), timeoutMS);
    } catch {
      failWorker("浏览器无法启动棋局引擎，请更新浏览器后重试。");
    }
  });
  return initialization;
}

export async function runGame(payload) {
  if (document.documentElement.dataset.engine === "wasm") {
    await initializeEngine();
    return new Promise((resolve, reject) => {
      const id = ++sequence;
      const timeout = setTimeout(() => failWorker("等待棋局超时，请刷新后重试。当前棋局已保留。"), timeoutMS);
      pending.set(id, { resolve, reject, timeout });
      try {
        worker.postMessage({ id, payload });
      } catch {
        failWorker("无法提交棋局，请刷新后重试。当前棋局已保留。");
      }
    });
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMS);
  try {
    const response = await fetch("./api/game", {
      method: "POST",
      signal: controller.signal,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
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
