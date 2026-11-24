import {renderBoard} from "./board.js";
import {createPlayer} from "./open-players.js";
import {createReferee} from "./open-referee.js";

const colors = ["black", "white"];
const sides = {black:"黑棋", white:"白棋"};
const seatNames = {cpu:"Yi Club Fighter", chat:"自带密钥模型", agent:"自有 Agent"};
const emptyBoard = {size:9, board:Array(81).fill(0), moves:[], lastMove:null, captures:{black:0, white:0}};
const timeLimitMS = 30000;

export function callBudget(kinds, maxMoves) {
  return {
    black: kinds.black === "cpu" ? 0 : Math.ceil(maxMoves / 2),
    white: kinds.white === "cpu" ? 0 : Math.floor(maxMoves / 2),
  };
}

// Only referee-accepted moves and controlled labels enter the portable record.
export function makeSGF(record) {
  const labels = {cpu:"Yi Club Fighter", chat:"BYOK model", agent:"HTTP agent"};
  const result = record.result;
  let header = "(;GM[1]FF[4]CA[UTF-8]SZ[9]KM[6.5]RU[Area scoring, simple ko]";
  header += "PB[" + labels[record.kinds.black] + "]PW[" + labels[record.kinds.white] + "]";
  if (result) {
    const suffix = result.reason === "resign" ? "R" : result.score.margin;
    const outcome = result.winner === "draw" ? "0" : (result.winner === "black" ? "B+" : "W+") + suffix;
    header += "RE[" + outcome + "]";
  }
  header += "C[Yi Arena exhibition. Move cap " + record.maxMoves + ". "
    + (result ? result.reason : "Unfinished") + ".]";
  const nodes = record.moves.filter(move => move !== "resign").map((move, index) => {
    const vertex = move === "pass" ? "" :
      String.fromCharCode(97 + "ABCDEFGHJ".indexOf(move[0]), 97 + 9 - Number(move.slice(1)));
    return ";" + (index % 2 === 0 ? "B" : "W") + "[" + vertex + "]";
  });
  return header + "\n" + nodes.join("") + ")\n";
}

export async function playGame(ref, players, record, signal, onTurn, onMove) {
  const budget = callBudget(record.kinds, record.maxMoves);
  while (!ref.result()) {
    signal.throwIfAborted();
    const turn = ref.turn();
    const color = turn.color;
    const player = players[color];
    if (player) {
      if (record.calls[color] >= budget[color]) throw new Error("本席位已达到请求上限。");
      record.calls[color]++;
    }
    onTurn(color);
    const move = player ? await player.choose({
      ...turn, id:record.gameId + ":" + turn.revision, gameId:record.gameId, timeLimitMS,
    }, signal) : await ref.cpu();
    signal.throwIfAborted();
    await ref.play(move);
    signal.throwIfAborted();
    record.moves.push(move);
    record.result = ref.result();
    await onMove(ref.snapshot());
  }
}

function setupPage() {
  const $ = id => document.getElementById(id);
  const controls = {};
  let running = null;
  let record = null;
  let previous = null;

  function seatKinds() {
    return Object.fromEntries(colors.map(color => [color, controls[color].querySelector(".kind").value]));
  }
  function updateBudget() {
    const maxMoves = $("max-moves").valueAsNumber;
    if (!$("max-moves").validity.valid) {
      $("budget").textContent = "请输入 2 到 144 之间的整数手数。";
      return;
    }
    const budget = callBudget(seatKinds(), maxMoves);
    $("budget").textContent = "外部请求上限 · 黑 " + budget.black + " 次 / 白 " + budget.white
      + " 次。每手限时 30 秒，达到手数上限按盘面计分。";
  }
  function display(board, move = "") {
    renderBoard(board, previous, !!move);
    previous = board;
    const last = move === "pass" ? "停一手" : move === "resign" ? "认输" : move;
    $("ply").textContent = "第 " + board.moves.length + " 手" + (last ? " · " + last : "");
    $("captures").textContent = "提子　黑 " + board.captures.black + "　白 " + board.captures.white;
    $("board").setAttribute("aria-label", "九路棋盘，" + $("ply").textContent + "，" + $("captures").textContent);
  }
  function callCount() {
    $("calls").textContent = "外部请求 · 黑 " + record.calls.black + " 次 / 白 " + record.calls.white + " 次";
  }
  function clearKeys() {
    for (const seat of Object.values(controls)) seat.querySelector(".key").value = "";
  }

  for (const color of colors) {
    const fragment = $("seat-template").content.cloneNode(true);
    const seat = fragment.querySelector("fieldset");
    const kind = seat.querySelector(".kind");
    const url = seat.querySelector(".endpoint");
    const model = seat.querySelector(".model");
    const key = seat.querySelector(".key");
    seat.id = color + "-config";
    seat.querySelector("legend span").textContent = color === "black" ? "黑棋 · 先行" : "白棋";
    seat.querySelector(".seat-stone").classList.add(color);
    kind.setAttribute("aria-label", sides[color] + "席位类型");
    kind.value = color === "black" ? "chat" : "cpu";
    function syncSeat(reset) {
      const chat = kind.value === "chat";
      const external = kind.value !== "cpu";
      if (reset) { key.value = ""; url.value = ""; }
      seat.querySelector(".external-fields").hidden = !external;
      seat.querySelector(".model-field").hidden = !chat;
      seat.querySelector(".url-label").textContent = chat ? "兼容 Chat Completions 的 API 基址" : "Agent 回合接口地址";
      seat.querySelector(".key-label").textContent = chat ? "API 密钥" : "Bearer 令牌";
      url.placeholder = chat ? "https://api.example.com/v1" : "https://agent.example.org/turn";
      url.disabled = key.disabled = !external;
      url.required = external;
      model.disabled = !chat;
      model.required = chat;
    }
    kind.addEventListener("change", () => {
      syncSeat(true);
      if (!record) $(color + "-name").textContent = seatNames[kind.value];
      updateBudget();
    });
    url.addEventListener("input", () => { key.value = ""; });
    controls[color] = seat;
    syncSeat(false);
    $("seats").append(fragment);
  }
  $("max-moves").addEventListener("input", updateBudget);
  updateBudget();
  display(emptyBoard);
  $("start").disabled = false;

  $("setup-form").addEventListener("submit", async event => {
    event.preventDefault();
    if (running || !$("setup-form").reportValidity()) return;
    const kinds = seatKinds();
    const players = {};
    let side = "";
    $("error").hidden = true;
    try {
      for (const color of colors) {
        side = color;
        const seat = controls[color];
        if (kinds[color] !== "cpu") players[color] = createPlayer({
          kind:kinds[color], baseURL:seat.querySelector(".endpoint").value.trim(),
          model:seat.querySelector(".model").value.trim(), key:seat.querySelector(".key").value,
        }, color);
      }
    } catch (error) {
      clearKeys();
      $("error").textContent = sides[side] + " · " + error.message;
      $("error").hidden = false;
      $("error").scrollIntoView({block:"nearest"});
      return;
    }

    const maxMoves = $("max-moves").valueAsNumber;
    const controller = new AbortController();
    running = controller;
    record = {gameId:Array.from(crypto.getRandomValues(new Uint8Array(16)), n => n.toString(16).padStart(2,"0")).join(""),
      kinds, maxMoves, moves:[], calls:{black:0, white:0}, result:null};
    clearKeys();
    for (const color of colors) {
      controls[color].disabled = true;
      $(color + "-name").textContent = kinds[color] === "chat"
        ? controls[color].querySelector(".model").value.trim() : seatNames[kinds[color]];
    }
    $("limits").disabled = $("start").disabled = true;
    $("download").hidden = true;
    $("stop").hidden = false;
    $("run-state").hidden = false;
    $("status").textContent = "裁判正在准备棋盘…";
    previous = null;
    display(emptyBoard);
    callCount();
    if (matchMedia("(max-width: 760px)").matches) $("table").scrollIntoView({block:"start"});
    side = "";

    try {
      const ref = await createReferee(maxMoves);
      await playGame(ref, players, record, controller.signal, color => {
        side = color;
        $("status").textContent = sides[color] + "正在思考…";
        callCount();
      }, async board => {
        display(board, record.moves.at(-1));
        if (!record.result) await new Promise(resolve => setTimeout(resolve, 300));
      });
      const {winner, reason, score} = record.result;
      const who = winner === "draw" ? "和棋" : sides[winner] + "胜" + (score ? " " + score.margin + " 目" : "");
      const why = {"passes":"双方停一手", "move-cap":"达到手数上限", "resign":"对手认输"}[reason];
      $("status").textContent = who + " · " + why;
    } catch (error) {
      if (controller.signal.aborted) {
        $("status").textContent = "已停止 · 棋谱保留至最后一手";
      } else {
        $("status").textContent = "对局中断 · 已保留棋谱";
        $("error").textContent = (side ? sides[side] + " · " : "") + error.message;
        $("error").hidden = false;
      }
    } finally {
      running = null;
      for (const color of colors) controls[color].disabled = false;
      $("limits").disabled = $("start").disabled = false;
      $("start").firstChild.textContent = "重新开局 ";
      $("stop").hidden = true;
      $("download").hidden = false;
    }
  });

  $("stop").addEventListener("click", () => {
    running?.abort();
    $("status").textContent = "正在停止对局…";
  });
  window.addEventListener("pagehide", () => { running?.abort(); clearKeys(); });
  $("download").addEventListener("click", () => {
    if (!record || running) return;
    const url = URL.createObjectURL(new Blob([makeSGF(record)], {type:"application/x-go-sgf"}));
    const link = document.createElement("a");
    link.href = url;
    link.download = "yi-exhibition-" + record.gameId + ".sgf";
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
}

if (typeof document !== "undefined") setupPage();
