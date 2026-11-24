import { initializeEngine, runGame } from "./engine.js";
import { renderBoard } from "./board.js";

const $ = (id) => document.getElementById(id);
const board = $("board");
const storageKey = "yi-game-v1";
const letters = "ABCDEFGHJKLMNOPQRST";
const sizeNames = { 9: "九路棋盘", 13: "十三路棋盘", 19: "十九路棋盘" };
let opponents = [];
let game = { size: 9, style: "china", board: Array(81).fill(0), next: 1, moves: [], lastMove: null, captures: { black: 0, white: 0 }, over: false, resigned: 0 };
let engineReady = false;
let engineFailed = false;
let busy = "";
let restoreFailed = false;
let cursor = { x: 4, y: 4 };
let keyboard = false;
let hover = null;

function position(n) { return 48 + n * 544 / (game.size - 1); }
function coordinate(x, y) { return `${letters[x]}${game.size - y}`; }
function cursorDescription() { return `${coordinate(cursor.x, cursor.y)}，${["空点", "黑棋", "白棋"][game.board[cursor.y * game.size + cursor.x]]}`; }
function canPlay() { return engineReady && !busy && !restoreFailed && !game.over && game.next === 1; }
function selectedOpponent() { return opponents.find(opponent => opponent.id === game.style); }

function loadOpponents(roster) {
  if (!Array.isArray(roster) || !roster.length || roster.some(opponent => !opponent || ["id", "name", "group", "description"].some(key => typeof opponent[key] !== "string" || !opponent[key].trim())) || new Set(roster.map(opponent => opponent.id)).size !== roster.length) throw new Error("对手名单不完整，请刷新后重试。");
  const groups = new Map();
  $("style").replaceChildren();
  for (const opponent of roster) {
    if (!groups.has(opponent.group)) {
      const group = document.createElement("optgroup");
      group.label = opponent.group;
      groups.set(opponent.group, group);
      $("style").append(group);
    }
    const option = document.createElement("option");
    option.value = opponent.id;
    option.textContent = opponent.name;
    groups.get(opponent.group).append(option);
  }
  opponents = roster;
}

function validateGame(data) {
  if (!data || ![9, 13, 19].includes(data.size) || !opponents.some(opponent => opponent.id === data.style) || !Array.isArray(data.board) || data.board.length !== data.size * data.size || !Array.isArray(data.moves) || !data.captures) throw new Error("棋局数据不完整，请刷新后重试。");
}

function draw(previous = null, animateTurn = false) {
  renderBoard(game, previous, animateTurn);
  cursor.x = Math.min(cursor.x, game.size - 1);
  cursor.y = Math.min(cursor.y, game.size - 1);
  render();
}

function preview() {
  const stone = busy === "play" ? cursor : (canPlay() && hover);
  $("ghost").setAttribute("opacity", busy === "play" ? ".5" : ".22");
  for (const [id, point, visible] of [["ghost", stone, stone && !game.board[stone.y * game.size + stone.x]], ["cursor", cursor, keyboard && canPlay()]]) {
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
  const opponent = selectedOpponent();
  $("size").value = game.size;
  if (opponent) {
    $("style").value = opponent.id;
    $("style-detail").textContent = opponent.description;
    $("cpu-detail").hidden = !game.search;
    $("cpu-detail").textContent = game.search ? `${game.search.workers} 路搜索 · 读至 ${game.search.depth} 手` : "";
  }
  $("move-count").textContent = `第 ${game.moves.length} 手`;
  $("black-captures").textContent = game.captures.black;
  $("white-captures").textContent = game.captures.white;
  $("undo").disabled = !engineReady || busy || !game.moves.length || restoreFailed;
  $("pass").disabled = !canPlay();
  $("resign").disabled = !canPlay();
  $("new-game").disabled = !engineReady || busy;
  $("size").disabled = !engineReady || busy;
  $("style").disabled = !engineReady || busy || restoreFailed;
  board.setAttribute("aria-disabled", String(!canPlay()));
  let title = "轮到你了";
  let detail = "";
  if (!engineReady) title = engineFailed ? "棋局载入失败" : "正在载入棋局…";
  else if (busy) {
    title = {
      play: `${opponent.name}思考中…`, pass: `${opponent.name}思考中…`,
      resign: "正在结束棋局…", restore: "正在恢复棋局…", opponent: "正在切换对手…",
      undo: "正在悔棋…", new: "正在开局…", sync: "正在更新棋局…",
    }[busy];
  }
  else if (restoreFailed) { title = "棋局尚未恢复"; detail = "刷新重试，或开始新对局。"; }
  else if (game.resigned) title = "你已认输，白棋胜";
  else if (game.over && game.score) {
    title = `${game.score.winner === 1 ? "黑棋" : "白棋"}胜 ${game.score.margin} 目`;
    detail = `黑 ${game.score.black} · 白 ${game.score.white}（含贴目）`;
  } else if (game.moves.length) {
    detail = game.lastMove ? `白棋 · ${coordinate(game.lastMove.x, game.lastMove.y)}` : "白棋停一手";
  } else title = "执黑，点交叉处落子";
  $("status").textContent = title;
  $("status-detail").textContent = detail;
  $("status-detail").hidden = !detail;
  board.setAttribute("aria-label", `${sizeNames[game.size]}，${title}，光标 ${cursorDescription()}`);
  preview();
}

function showError(message) {
  $("error").textContent = message;
  $("error").hidden = !message;
}

async function request(payload, activity = payload.action) {
  if (busy || !engineReady) return false;
  busy = activity;
  showError("");
  render();
  try {
    const data = await runGame({ style: game.style, ...payload });
    validateGame(data);
    const previousGame = game;
    game = data;
    restoreFailed = false;
    hover = null;
    try {
      localStorage.setItem(storageKey, JSON.stringify({ size: game.size, style: game.style, moves: game.moves, resigned: game.resigned || 0 }));
    } catch {
      $("save-note").textContent = "浏览器无法保存，关闭页面后棋局会丢失";
    }
    draw(previousGame, payload.action === "play" || payload.action === "pass");
    return true;
  } catch (error) {
    showError(error.message || "这一步没有成功，请重试。当前棋局已保留。");
    return false;
  } finally {
    busy = "";
    render();
  }
}

function act(action, point = {}) {
  if (!canPlay()) return;
  if (action === "play") { cursor = point; hover = null; }
  return request({ size: game.size, moves: game.moves, action, ...point });
}

async function newGame(size) {
  if (game.moves.length && !game.over && !window.confirm("开始新对局？当前棋局将被替换。")) {
    $("size").value = game.size;
    return;
  }
  if (await request({ size, moves: [], action: "sync" }, "new")) {
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
  if (event.pointerType === "touch" || !canPlay()) return;
  hover = pointerPoint(event);
  preview();
});
board.addEventListener("pointerleave", () => { hover = null; preview(); });
board.addEventListener("pointerdown", () => { keyboard = false; preview(); });
board.addEventListener("click", (event) => {
  const point = pointerPoint(event);
  if (point) act("play", point);
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
$("style").addEventListener("change", (event) => request({ size: game.size, style: event.target.value, moves: game.moves, resigned: game.resigned || 0, action: "sync" }, "opponent"));
$("undo").addEventListener("click", () => {
  const count = game.moves.length % 2 === 0 ? 2 : 1;
  request({ size: game.size, moves: game.moves.slice(0, -count), action: "sync" }, "undo");
});

async function start() {
  draw();
  try {
    await initializeEngine();
    const data = await runGame({ size: 9, style: "china", moves: [], action: "sync", includeOpponents: true });
    loadOpponents(data.opponents);
    validateGame(data);
    game = data;
    engineReady = true;
  } catch (error) {
    engineFailed = true;
    showError(error.message || "棋局载入失败，请刷新重试。");
    render();
    return;
  }
  let saved;
  try { saved = JSON.parse(localStorage.getItem(storageKey)); } catch (error) {
    if (error instanceof SyntaxError) {
      restoreFailed = true;
      showError("本地棋局无法读取。请点击「新对局」重新开始。");
      render();
      return;
    }
    $("save-note").textContent = "浏览器无法保存，关闭页面后棋局会丢失";
  }
  if (!saved) { render(); return; }
  restoreFailed = true;
  await request({ size: saved.size, style: saved.style || "china", moves: saved.moves, resigned: saved.resigned || 0, action: "sync" }, "restore");
}
start();
