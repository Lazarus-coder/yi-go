let ready = false;

self.onmessage = ({ data }) => {
  if (!ready) {
    self.postMessage({ id: data.id, error: "棋局引擎尚未就绪，请稍候再试。" });
    return;
  }
  try {
    const run = { run: self.yiRun, analyze: self.yiAnalyze, merge: self.yiMerge, exhibition: self.yiExhibition }[data.operation || "run"];
    if (!run) throw new Error("Unknown worker operation");
    const result = JSON.parse(run(JSON.stringify(data.payload)));
    self.postMessage({ id: data.id, ...result });
  } catch {
    self.postMessage({ type: "workererror" });
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
