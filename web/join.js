import {renderBoard} from "./board.js";
import {createPlayer} from "./open-players.js";
import {createReferee} from "./open-referee.js";

const colors = ["black", "white"];
const sides = {black:"黑棋", white:"白棋"};
const seatNames = {cpu:"Yi Club Fighter", chat:"BYOK 模型", agent:"自有 Agent"};
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
    return Object.fromEntries(colors.map(color => [color, controls[color].querySelector(".kind:checked").value]));
  }
  function updateBudget() {
    const kinds = seatKinds();
    const seats = Object.values(kinds);
    const modelSeat = seats.includes("chat");
    const agentSeat = seats.includes("agent");
    $("privacy").hidden = $("budget").hidden = $("setup-links").hidden = !(modelSeat || agentSeat);
    $("agent-guide").hidden = !agentSeat;
    $("model-guide").hidden = !modelSeat;
    $("privacy").textContent = modelSeat
      ? "密钥仅发给所填服务。已发出的请求可能计费。" : "令牌仅发给所填 Agent。";
    const cap = $("max-moves");
    if (!cap.validity.valid) {
      $("budget").textContent = "请输入 2 至 144 的整数。";
      return;
    }
    const budget = callBudget(kinds, cap.valueAsNumber);
    $("budget").textContent = "请求上限 · 黑 " + budget.black + " 次 / 白 " + budget.white + " 次";
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
    const kinds = seat.querySelectorAll(".kind");
    const url = seat.querySelector(".endpoint");
    const model = seat.querySelector(".model");
    const key = seat.querySelector(".key");
    seat.id = color + "-config";
    seat.querySelector("legend span").textContent = color === "black" ? "黑棋 · 先行" : "白棋";
    seat.querySelector(".seat-stone").classList.add(color);
    for (const radio of kinds) radio.name = color + "-kind";
    function syncSeat(reset) {
      const kind = seat.querySelector(".kind:checked").value;
      const chat = kind === "chat";
      const external = kind !== "cpu";
      if (reset) { key.value = ""; url.value = ""; }
      seat.querySelector(".external-fields").hidden = !external;
      seat.querySelector(".model-field").hidden = !chat;
      seat.querySelector(".url-label").textContent = chat ? "Chat Completions 基址" : "Agent 接口地址";
      seat.querySelector(".key-label").textContent = chat ? "API 密钥" : "Bearer 令牌";
      url.placeholder = chat ? "https://api.example.com/v1" : "https://agent.example.org/turn";
      url.disabled = key.disabled = !external;
      url.required = external;
      model.disabled = !chat;
      model.required = chat;
    }
    for (const radio of kinds) radio.addEventListener("change", () => {
      syncSeat(true);
      updateBudget();
    });
    url.addEventListener("input", () => { key.value = ""; });
    controls[color] = seat;
    syncSeat(false);
    $("seats").append(fragment);
  }
  $("max-moves").addEventListener("input", updateBudget);
  $("max-moves").addEventListener("invalid", event => { event.target.closest("details").open = true; });
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
    $("match-seats").hidden = $("board-state").hidden = $("run-state").hidden = false;
    $("status").textContent = "准备开局…";
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
        $("status").textContent = "对局已停止";
      } else {
        $("status").textContent = "对局中断";
        $("error").textContent = (side ? sides[side] + " · " : "") + error.message;
        $("error").hidden = false;
      }
    } finally {
      running = null;
      for (const color of colors) controls[color].disabled = false;
      $("limits").disabled = $("start").disabled = false;
      $("start").firstChild.textContent = "再开一局 ";
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
