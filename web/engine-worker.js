let ready = false;

self.onmessage = ({ data }) => {
  if (!ready) {
    self.postMessage({ id: data.id, error: "棋局引擎尚未就绪，请稍候再试。" });
    return;
  }
  try {
    const result = JSON.parse(self.yiRun(JSON.stringify(data.payload)));
    self.postMessage({ id: data.id, ...result });
  } catch {
    self.postMessage({ id: data.id, error: "棋局计算失败，请刷新后重试。当前棋局已保留。" });
  }
};

async function start() {
  try {
    importScripts("./wasm_exec.js");
    const go = new Go();
    self.yiReady = () => {
      ready = true;
      self.postMessage({ type: "ready" });
    };
    const response = await fetch("./game.wasm");
    if (!response.ok) throw new Error("WASM download failed");
    // arrayBuffer also works with static servers that omit the WASM MIME type.
    const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    await go.run(instance);
    throw new Error("Go runtime stopped");
  } catch {
    ready = false;
    self.postMessage({ type: "initerror" });
  }
}

start();
