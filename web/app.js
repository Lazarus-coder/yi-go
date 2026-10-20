import { initializeEngine, runGame } from "./engine.js";

const $ = (id) => document.getElementById(id);
const board = $("board");
const storageKey = "yi-game-v1";
const letters = "ABCDEFGHJKLMNOPQRST";
const sizeNames = { 9: "九路棋盘", 13: "十三路棋盘", 19: "十九路棋盘" };
const styleDescriptions = {
  china: "兼顾厚势、连接与攻守平衡。",
  japan: "重视布局、角边实地与棋形。",
  korea: "偏好贴身战斗、打吃与主动进攻。",
};
let game = { size: 9, style: "china", board: Array(81).fill(0), next: 1, moves: [], lastMove: null, captures: { black: 0, white: 0 }, over: false, resigned: 0 };
let engineReady = false;
let engineFailed = false;
let busy = false;
let restoreFailed = false;
let cursor = { x: 4, y: 4 };
let keyboard = false;
let hover = null;

function svg(tag, attributes) {
  const element = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, value);
  return element;
}

function position(n) { return 48 + n * 544 / (game.size - 1); }
function coordinate(x, y) { return `${letters[x]}${game.size - y}`; }
function cursorDescription() { return `${coordinate(cursor.x, cursor.y)}，${["空点", "黑棋", "白棋"][game.board[cursor.y * game.size + cursor.x]]}`; }
function canPlay() { return engineReady && !busy && !restoreFailed && !game.over && game.next === 1; }

function draw() {
  const grid = $("grid");
  const stones = $("stones");
  grid.replaceChildren();
  stones.replaceChildren();
  const radius = 544 / (game.size - 1) * .44;
  for (let i = 0; i < game.size; i++) {
    const p = position(i);
    grid.append(svg("path", { d: `M48 ${p}H592 M${p} 48V592`, stroke: "#6f5939", "stroke-width": i === 0 || i === game.size - 1 ? 1.1 : .75, opacity: ".67", fill: "none" }));
    for (const [x, y, text] of [[p, 28, letters[i]], [p, 619, letters[i]], [25, p + 4, game.size - i], [615, p + 4, game.size - i]]) {
      const label = svg("text", { x, y, "text-anchor": "middle", fill: "#665136", "font-size": 11, "font-family": "system-ui, sans-serif" });
      label.textContent = text;
      grid.append(label);
    }
  }
  const star = game.size === 9 ? [2, 6] : game.size === 13 ? [3, 9] : [3, 9, 15];
  for (const y of star) for (const x of star) grid.append(svg("circle", { cx: position(x), cy: position(y), r: 2.5, fill: "#705936" }));
  if (game.size !== 19) grid.append(svg("circle", { cx: 320, cy: 320, r: 2.5, fill: "#705936" }));
  game.board.forEach((color, index) => {
    if (!color) return;
    const x = index % game.size;
    const y = Math.floor(index / game.size);
    stones.append(svg("circle", { cx: position(x), cy: position(y), r: radius, fill: `url(#${color === 1 ? "black" : "white"}-stone)`, filter: "url(#stone-shadow)" }));
    if (game.lastMove?.x === x && game.lastMove?.y === y) stones.append(svg("circle", { cx: position(x), cy: position(y), r: Math.max(2.4, radius * .17), fill: "none", stroke: color === 1 ? "#e5d7b8" : "#a54932", "stroke-width": 1.6 }));
  });
  cursor.x = Math.min(cursor.x, game.size - 1);
  cursor.y = Math.min(cursor.y, game.size - 1);
  render();
}

function preview() {
  for (const [id, point, visible] of [["ghost", hover, canPlay() && hover && !game.board[hover.y * game.size + hover.x]], ["cursor", cursor, keyboard && canPlay()]]) {
    const marker = $(id);
    marker.toggleAttribute("hidden", !visible);
    if (point) {
      marker.setAttribute("cx", position(point.x));
      marker.setAttribute("cy", position(point.y));
      marker.setAttribute("r", 544 / (game.size - 1) * (id === "ghost" ? .44 : .47));
    }
  }
}

function render() {
  $("size").value = game.size;
  $("style").value = game.style;
  $("style-detail").textContent = styleDescriptions[game.style];
  $("board-size").textContent = sizeNames[game.size];
  $("move-count").textContent = `第 ${game.moves.length} 手`;
  $("black-captures").textContent = game.captures.black;
  $("white-captures").textContent = game.captures.white;
  $("human-turn").hidden = !canPlay();
  $("cpu-turn").hidden = !busy;
  $("undo").disabled = !engineReady || busy || !game.moves.length || restoreFailed;
  $("pass").disabled = !canPlay();
  $("resign").disabled = !canPlay();
  $("new-game").disabled = !engineReady || busy;
  $("size").disabled = !engineReady || busy;
  $("style").disabled = !engineReady || busy || restoreFailed;
  board.setAttribute("aria-disabled", String(!canPlay()));
  let title = "轮到你了";
  let detail = game.lastMove ? `CPU 落在 ${coordinate(game.lastMove.x, game.lastMove.y)}。轮到你执黑。` : "选择一个交叉点，落下第一颗棋子。";
  if (!engineReady) {
    title = engineFailed ? "棋局未能载入" : "正在载入棋局…";
    detail = engineFailed ? "请刷新页面，重新载入后继续。" : "准备就绪后，即可落子。";
  }
  else if (busy) { title = "正在计算棋局…"; detail = "请稍候，CPU 正在思考。"; }
  else if (restoreFailed) { title = "棋局尚未恢复"; detail = "请刷新重试，或点击「新对局」重新开始。"; }
  else if (game.resigned) { title = "白棋获胜"; detail = "你已认输。下一局，重新开始。"; }
  else if (game.over && game.score) {
    title = `${game.score.winner === 1 ? "黑棋" : "白棋"}胜 ${game.score.margin} 目`;
    detail = `盘面估算：黑 ${game.score.black} · 白 ${game.score.white}（含贴目）。未判定死子。`;
  } else if (game.moves.length && game.moves.at(-1).x === -1) {
    detail = "CPU 停了一手。你可以继续落子，或停一手结束。";
  }
  $("status").textContent = title;
  $("status-detail").textContent = detail;
  board.setAttribute("aria-label", `${sizeNames[game.size]}，${title}，光标 ${cursorDescription()}。使用方向键移动，回车或空格落子。`);
  preview();
}

function showError(message) {
  $("error").textContent = message;
  $("error").hidden = !message;
}

async function request(payload) {
  if (busy || !engineReady) return false;
  busy = true;
  showError("");
  render();
  try {
    const data = await runGame({ style: game.style, ...payload });
    if (![9, 13, 19].includes(data.size) || !Object.hasOwn(styleDescriptions, data.style) || !Array.isArray(data.board) || data.board.length !== data.size * data.size || !Array.isArray(data.moves) || !data.captures) throw new Error("棋局数据不完整，请刷新后重试。");
    game = data;
    restoreFailed = false;
    hover = null;
    try {
      localStorage.setItem(storageKey, JSON.stringify({ size: game.size, style: game.style, moves: game.moves, resigned: game.resigned || 0 }));
    } catch {
      $("save-note").textContent = "浏览器无法保存，关闭页面后棋局会丢失";
    }
    draw();
    return true;
  } catch (error) {
    showError(error.message || "这一步没有成功，请重试。当前棋局已保留。");
    return false;
  } finally {
    busy = false;
    render();
  }
}

function act(action, point = {}) {
  if (!canPlay()) return;
  return request({ size: game.size, moves: game.moves, action, ...point });
}

async function newGame(size) {
  if (game.moves.length && !game.over && !window.confirm("开始新对局？当前棋局将被替换。")) {
    $("size").value = game.size;
    return;
  }
  if (await request({ size, moves: [], action: "sync" })) {
    cursor = { x: Math.floor(size / 2), y: Math.floor(size / 2) };
    render();
  }
}

function pointerPoint(event) {
  const point = board.createSVGPoint();
  point.x = event.clientX;
  point.y = event.clientY;
  const local = point.matrixTransform(board.getScreenCTM().inverse());
  const step = 544 / (game.size - 1);
  const x = Math.round((local.x - 48) / step);
  const y = Math.round((local.y - 48) / step);
  return x >= 0 && x < game.size && y >= 0 && y < game.size ? { x, y } : null;
}

board.addEventListener("pointermove", (event) => {
  if (event.pointerType === "touch") return;
  hover = pointerPoint(event);
  preview();
});
board.addEventListener("pointerleave", () => { hover = null; preview(); });
board.addEventListener("pointerdown", () => { keyboard = false; preview(); });
board.addEventListener("click", (event) => {
  const point = pointerPoint(event);
  if (point) { cursor = point; act("play", point); }
});
board.addEventListener("focus", () => { keyboard = board.matches(":focus-visible"); preview(); });
board.addEventListener("blur", () => { keyboard = false; preview(); });
board.addEventListener("keydown", (event) => {
  const directions = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
  if (!directions[event.key] && event.key !== "Enter" && event.key !== " ") return;
  event.preventDefault();
  if (!canPlay()) return;
  keyboard = true;
  hover = null;
  if (directions[event.key]) {
    const [dx, dy] = directions[event.key];
    cursor.x = Math.max(0, Math.min(game.size - 1, cursor.x + dx));
    cursor.y = Math.max(0, Math.min(game.size - 1, cursor.y + dy));
    render();
    $("board-announcement").textContent = cursorDescription();
  } else if (!event.repeat) act("play", cursor);
});
$("pass").addEventListener("click", () => act("pass"));
$("resign").addEventListener("click", () => {
  if (window.confirm("确认认输，结束这一局？")) act("resign");
});
$("new-game").addEventListener("click", () => newGame(game.size));
$("size").addEventListener("change", (event) => newGame(Number(event.target.value)));
$("style").addEventListener("change", (event) => request({ size: game.size, style: event.target.value, moves: game.moves, resigned: game.resigned || 0, action: "sync" }));
$("undo").addEventListener("click", () => {
  const count = game.moves.length % 2 === 0 ? 2 : 1;
  request({ size: game.size, moves: game.moves.slice(0, -count), action: "sync" });
});

async function start() {
  draw();
  try {
    await initializeEngine();
    engineReady = true;
  } catch (error) {
    engineFailed = true;
    showError(error.message || "棋局载入失败，请刷新重试。");
    render();
    return;
  }
  let saved;
  try { saved = JSON.parse(localStorage.getItem(storageKey)); } catch { /* A fresh game still works without browser storage. */ }
  if (!saved) { render(); return; }
  restoreFailed = true;
  await request({ size: saved.size, style: saved.style || "china", moves: saved.moves, resigned: saved.resigned || 0, action: "sync" });
}
start();
