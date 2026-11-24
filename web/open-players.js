const maxBytes = 64 * 1024;
const protocol = "yi-agent-v1";
const prompt = 'You are playing Go. Choose exactly one move from legal to maximize your final area score. X is Black, O is White. Reply with just a coordinate, pass, resign, or a JSON object {"move":"D4"}.';

class PlayerError extends Error {}

function endpointURL(value, chat) {
  let url;
  try { url = new URL(value); } catch { throw new PlayerError("请填写完整的服务地址。"); }
  const local = ["localhost", "127.0.0.1"].includes(url.hostname);
  if (url.protocol !== "https:" && !(local && url.protocol === "http:")) {
    throw new PlayerError("请使用 HTTPS，或使用 http://127.0.0.1 与 http://localhost 连接本机服务。");
  }
  if (url.username || url.password || /[?#]/.test(url.href)) {
    throw new PlayerError("服务地址不能包含登录信息、查询参数或片段。请在密钥栏填写凭据。");
  }
  if (chat) url.pathname = url.pathname.replace(/\/+$/, "") + "/chat/completions";
  return url;
}

async function readJSON(response) {
  if (!response.body) throw new PlayerError("服务返回了空响应。");
  if (Number(response.headers.get("content-length")) > maxBytes) throw new PlayerError("服务响应超过 64 KiB。");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let count = 0, text = "";
  try {
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      count += value.byteLength;
      if (count > maxBytes) throw new PlayerError("服务响应超过 64 KiB。");
      text += decoder.decode(value, {stream:true});
    }
  } finally { reader.releaseLock(); }
  try { return JSON.parse(text + decoder.decode()); }
  catch { throw new PlayerError("服务返回的内容不是有效 JSON。"); }
}

function modelMove(response) {
  let content = response?.choices?.[0]?.message?.content;
  if (Array.isArray(content)) content = content.filter(part => part?.type === "text").map(part => part.text).join("");
  if (typeof content !== "string") throw new PlayerError("模型没有返回落子文本。请检查模型和接口格式。");
  const text = content.trim().replace(/^```(?:json)?\s*([\s\S]*?)\s*```$/i, "$1").trim();
  try {
    const parsed = JSON.parse(text);
    return typeof parsed === "string" ? parsed : parsed?.move;
  } catch { return text; }
}

function serviceError(status) {
  if (status === 401 || status === 403) return "服务拒绝访问，请检查此席位的密钥和访问权限。";
  if (status === 429) return "服务已限流或额度用尽。";
  if (status === 400 || status === 404 || status === 422) return "请检查接口路径、模型名称和请求格式。";
  return "请检查服务状态。";
}

export function createPlayer({kind, baseURL, model, key = ""}, color) {
  const chat = kind === "chat";
  if ((!chat && kind !== "agent") || !["black","white"].includes(color)) throw new PlayerError("请选择有效的席位类型与颜色。");
  const url = endpointURL(baseURL, chat);
  if (chat && (typeof model !== "string" || !model.trim())) throw new PlayerError("请填写服务提供的模型名称。");
  if (typeof key !== "string" || /[^\x21-\x7e]/.test(key)) throw new PlayerError("密钥含有无效字符。");
  const headers = {"Content-Type":"application/json"};
  if (key) headers.Authorization = "Bearer " + key;

  async function choose(turn, signal) {
    if (signal?.aborted) throw new DOMException("已停止本次请求。", "AbortError");
    if (turn.color !== color || !Array.isArray(turn.legal) || !turn.id || !turn.gameId) throw new PlayerError("当前棋局与席位不匹配。");
    const limit = Math.max(1, Math.min(120000, Math.floor(Number(turn.timeLimitMS) || 30000)));
    const {size, komi, ascii, history, legal, captures, rules, remainingMoves} = turn;
    const observation = {color, size, komi, ascii, history, legal, captures, rules, remainingMoves};
    const request = chat ? {
      model: model.trim(),
      [url.hostname === "api.openai.com" ? "max_completion_tokens" : "max_tokens"]: 256,
      messages: [{role:"system", content:prompt}, {role:"user", content:JSON.stringify(observation)}],
    } : {
      ...observation, type:"turn", protocol, id:turn.id, gameId:turn.gameId, revision:turn.revision,
      board:turn.board, maxMoves:turn.maxMoves, players:{black:"black",white:"white"}, turnTimeoutMS:limit, timeLimitMS:limit,
    };
    const controller = new AbortController();
    const abort = () => controller.abort();
    signal?.addEventListener("abort", abort, {once:true});
    const timer = setTimeout(abort, limit);
    try {
      const response = await fetch(url, {
        method:"POST", mode:"cors", credentials:"omit", redirect:"error", referrerPolicy:"no-referrer",
        cache:"no-store", headers, body:JSON.stringify(request), signal:controller.signal,
      });
      if (!response.ok) throw new PlayerError(`HTTP ${response.status} · ${serviceError(response.status)}`);
      const reply = await readJSON(response);
      if (!chat && (reply?.type !== "move" || reply.id !== turn.id || reply.revision !== turn.revision || Object.keys(reply).length !== 4)) {
        throw new PlayerError("Agent 的回复与当前请求编号或手数不符。");
      }
      const move = chat ? modelMove(reply) : reply.move;
      const canonical = typeof move === "string" && legal.find(item => item.toLowerCase() === move.trim().toLowerCase());
      if (!canonical) throw new PlayerError("此席位没有返回本手的合法坐标、pass 或 resign。");
      return canonical;
    } catch (error) {
      if (signal?.aborted) throw new DOMException("已停止本次请求。", "AbortError");
      if (controller.signal.aborted) throw new PlayerError("等待此席位落子超时。");
      if (error instanceof PlayerError) throw error;
      throw new PlayerError("无法连接到服务。请检查地址、网络及跨域访问（CORS）设置。");
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      controller.abort();
    }
  }
  return {choose};
}
