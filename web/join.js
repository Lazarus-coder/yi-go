import {renderBoard} from "./board.js";
import {createPlayer} from "./open-players.js";
import {createReferee} from "./open-referee.js";
import {readFork} from "./replay-fork.js";
import {cpuNames, decodeGame, encodeGame, isGameID, latestExhibition, saveKey} from "./exhibition-save.js";

const colors = ["black", "white"];
const sides = {black:"黑棋", white:"白棋"};
const seatNames = {cpu:"Yi Club Fighter", chat:"BYOK 模型", agent:"自有 Agent"};
const emptyBoard = {size:9, board:Array(81).fill(0), moves:[], lastMove:null, captures:{black:0, white:0}};
const timeLimitMS = 30000;

export function callBudget(kinds, maxMoves, from = 0) {
  return {
    black: kinds.black === "cpu" ? 0 : Math.ceil(maxMoves / 2) - Math.ceil(from / 2),
    white: kinds.white === "cpu" ? 0 : Math.floor(maxMoves / 2) - Math.floor(from / 2),
  };
}

// Only referee-accepted moves and controlled labels enter the portable record.
export function makeSGF(record) {
  const labels = {cpu:"Yi Club Fighter", chat:"BYOK model", agent:"HTTP agent"};
  const result = record.result;
  const size = record.size || 9;
  const label = color => record.seats?.[color]?.style === "alphago" ? "Yi Neural" : labels[record.kinds[color]];
  let header = "(;GM[1]FF[4]CA[UTF-8]SZ[" + size + "]KM[6.5]RU[Area scoring, simple ko]";
  header += "PB[" + label("black") + "]PW[" + label("white") + "]";
  if (result) {
    const suffix = result.reason === "resign" ? "R" : result.score.margin;
    const outcome = result.winner === "draw" ? "0" : (result.winner === "black" ? "B+" : "W+") + suffix;
    header += "RE[" + outcome + "]";
  }
  header += "C[Yi Arena exhibition. Move cap " + record.maxMoves + ". "
    + (result ? result.reason : "Unfinished") + ".]";
  const nodes = record.moves.filter(move => move !== "resign").map((move, index) => {
    const vertex = move === "pass" ? "" :
      String.fromCharCode(97 + "ABCDEFGHJKLMNOPQRST".indexOf(move[0]), 97 + size - Number(move.slice(1)));
    const forkNote = index + 1 === record.startPly ? "C[Exhibition seats take over from this position.]" : "";
    return ";" + (index % 2 === 0 ? "B" : "W") + "[" + vertex + "]" + forkNote;
  });
  return header + "\n" + nodes.join("") + ")\n";
}

export async function playGame(ref, players, record, signal, onTurn, onMove) {
  const remaining = callBudget(record.kinds, record.maxMoves, record.moves.length);
  while (!ref.result()) {
    signal.throwIfAborted();
    const turn = ref.turn();
    const color = turn.color;
    const player = players[color];
    if (player) {
      if (remaining[color] <= 0) throw new Error("本席位已达到请求上限。");
      remaining[color]--;
      record.calls[color]++;
      record.pending = color;
    }
    onTurn(color);
    const move = player ? await player.choose({
      ...turn, id:record.gameId + ":" + turn.revision, gameId:record.gameId, timeLimitMS,
    }, signal) : await ref.cpu(record.seats?.[color]?.style);
    signal.throwIfAborted();
    await ref.play(move);
    record.pending = null;
    record.moves.push(move);
    record.result = ref.result();
    await onMove(ref.snapshot());
    signal.throwIfAborted();
  }
}

function setupPage() {
  const $ = id => document.getElementById(id);
  const controls = {};
  let running = null;
  let record = null;
  let previous = null;
  let fork = null;
  let checkpoint = null;
  let storage;
  try {
    window.localStorage.getItem(latestExhibition);
    storage = window.localStorage;
  } catch { $("save-note").hidden = false; }
  const paused = () => record && !record.result;

  function save() {
    if (!storage) return;
    if (checkpoint && storage.getItem(saveKey(record.gameId)) !== checkpoint) {
      throw new Error("其他标签页已更新此局，请刷新后继续。");
    }
    const value = encodeGame(record);
    try {
      storage.setItem(saveKey(record.gameId), value);
      checkpoint = value;
    } catch { $("save-note").hidden = false; }
  }

  function rememberGame() {
    save();
    if (!storage || !checkpoint) return;
    try {
      storage.setItem(latestExhibition, record.gameId);
      const url = new URL(location.href);
      url.searchParams.delete("new");
      url.searchParams.set("game", record.gameId);
      url.hash = "";
      history.replaceState(null, "", url);
    } catch { $("save-note").hidden = false; }
  }


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
    const startPly = fork?.moves.length || 0;
    const budget = paused()
      ? callBudget(kinds, record.maxMoves, record.moves.length)
      : callBudget(kinds, startPly + cap.valueAsNumber, startPly);
    $("budget").textContent = "本次请求上限 · 黑 " + budget.black + " 次 / 白 " + budget.white + " 次";
  }
  function display(board, move = "", animate = false) {
    renderBoard(board, previous, animate);
    previous = board;
    const last = move === "pass" ? "停一手" : move === "resign" ? "认输" : move;
    $("ply").textContent = "第 " + board.moves.length + " 手" + (last ? " · " + last : "");
    $("captures").textContent = "提子　黑 " + board.captures.black + "　白 " + board.captures.white;
    $("board-size").textContent = board.size;
    $("board").setAttribute("aria-label", board.size + " 路棋盘，" + $("ply").textContent + "，" + $("captures").textContent);
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
    seat.querySelector("legend span").textContent = sides[color];
    seat.querySelector(".seat-stone").classList.add(color);
    for (const radio of kinds) radio.name = color + "-kind";
    function syncSeat(reset) {
      const kind = seat.querySelector(".kind:checked").value;
      const chat = kind === "chat";
      const external = kind !== "cpu";
      if (reset) { key.value = ""; url.value = ""; }
      seat.querySelector(".cpu-field").hidden = external;
      seat.querySelector(".cpu-style").disabled = external || !!paused();
      seat.querySelector(".external-fields").hidden = !external;
      seat.querySelector(".model-field").hidden = !chat;
      seat.querySelector(".url-label").textContent = chat ? "Chat Completions 基址" : "Agent 接口地址";
      seat.querySelector(".key-label").textContent = chat ? "API 密钥" : "Bearer 令牌";
      url.placeholder = chat ? "https://api.example.com/v1" : "https://agent.example.org/turn";
      url.disabled = key.disabled = !external;
      key.required = !!(external && paused() && record.seats[color].needsKey);
      key.placeholder = key.required ? "重新输入凭据" : "可选";
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
    seat.sync = syncSeat;
    syncSeat(false);
    $("seats").append(fragment);
  }
  $("max-moves").addEventListener("input", updateBudget);
  $("max-moves").addEventListener("invalid", event => { event.target.closest("details").open = true; });
  function refreshForm() {
    const resume = !!paused();
    $("start").firstChild.textContent = resume ? "从第 " + record.moves.length + " 手继续 " :
      fork ? "从此处开始 " : record ? "再开一局 " : "开始 Agent 对局 ";
    $("limits").disabled = resume;
    $("fresh").hidden = !(resume || fork);
    $("resume-note").textContent = record?.pending ? "上一请求可能已计费。继续将重新请求这一手。" : "";
    $("resume-note").hidden = !record?.pending;
    for (const seat of Object.values(controls)) {
      seat.disabled = false;
      for (const radio of seat.querySelectorAll(".kind")) radio.disabled = resume;
      seat.sync(false);
    }
    updateBudget();
  }

  function showPosition(ref) {
    $("board-state").hidden = $("run-state").hidden = false;
    display(ref.snapshot(), record?.moves.at(-1) || fork?.moves.at(-1));
    const result = ref.result();
    if (!result) {
      $("status").textContent = (record ? "已暂停" : "从此处开始") + " · 轮到" + sides[ref.turn().color];
      return;
    }
    const {winner, reason, score} = result;
    const who = winner === "draw" ? "和棋" : sides[winner] + "胜" + (score ? " " + score.margin + " 目" : "");
    const why = {"passes":"双方停一手", "move-cap":"达到手数上限", "resign":"对手认输"}[reason];
    $("status").textContent = who + " · " + why;
  }

  async function restore() {
    display(emptyBoard);
    fork = readFork(location.hash);
    const url = new URL(location.href);
    const id = url.searchParams.get("game") || (!fork && !url.searchParams.has("new") && storage?.getItem(latestExhibition));
    if (fork) {
      const ref = await createReferee(fork.moves.length + $("max-moves").valueAsNumber,
        {size:fork.size, history:fork.moves, startPly:fork.moves.length});
      if (ref.result()) throw new Error("这局棋已经结束，请回到上一手再试。");
      showPosition(ref);
    } else if (id) {
      if (!isGameID(id)) throw new Error("此局的本地存档无效。");
      checkpoint = storage?.getItem(saveKey(id));
      const saved = decodeGame(checkpoint, id);
      const ref = await createReferee(saved.maxMoves, {size:saved.size, history:saved.moves, startPly:saved.startPly});
      saved.result = ref.result();
      record = saved;
      if (saved.startPly) fork = {size:saved.size, moves:saved.moves.slice(0,saved.startPly)};
      $("max-moves").value = saved.maxMoves - saved.startPly;
      for (const color of colors) {
        const seat = controls[color], setting = saved.seats[color];
        seat.querySelector('.kind[value="' + setting.kind + '"]').checked = true;
        seat.sync(true);
        if (setting.kind === "cpu") seat.querySelector(".cpu-style").value = setting.style;
        else {
          seat.querySelector(".endpoint").value = setting.endpoint;
          if (setting.kind === "chat") seat.querySelector(".model").value = setting.model;
        }
      }
      callCount();
      showPosition(ref);
      $("download").hidden = false;
    }
    refreshForm();
    $("start").disabled = false;
  }
  restore().catch(error => {
    $("error").textContent = error.message;
    $("error").hidden = $("fresh").hidden = false;
  });

  $("fresh").addEventListener("click", () => {
    // Keep the previous game's save available in its own history entry.
    const url = new URL(location.href);
    url.hash = "";
    url.searchParams.delete("game");
    url.searchParams.set("new", "1");
    location.assign(url);
  });
  window.addEventListener("popstate", () => location.reload());

  $("setup-form").addEventListener("submit", async event => {
    event.preventDefault();
    if (running || !$("setup-form").reportValidity()) return;
    const resume = !!paused();
    const kinds = seatKinds(), seats = {}, players = {};
    let side = "";
    $("error").hidden = true;
    try {
      for (const color of colors) {
        side = color;
        const seat = controls[color], kind = kinds[color];
        if (kind === "cpu") {
          seats[color] = {kind, style:seat.querySelector(".cpu-style").value};
        } else {
          const endpoint = seat.querySelector(".endpoint").value.trim();
          const model = seat.querySelector(".model").value.trim();
          const key = seat.querySelector(".key").value;
          players[color] = createPlayer({kind, baseURL:endpoint, model, key}, color);
          // A pasted bearer may also occur in a private endpoint path or model alias.
          const publicField = value => key && value.includes(key) ? "" : value;
          seats[color] = {kind, endpoint:publicField(endpoint), needsKey:!!key, ...(kind === "chat" ? {model:publicField(model)} : {})};
        }
      }
    } catch (error) {
      clearKeys();
      $("error").textContent = sides[side] + " · " + error.message;
      $("error").hidden = false;
      $("error").scrollIntoView({block:"nearest"});
      return;
    }

    const startPly = fork?.moves.length || 0;
    const game = resume ? record : {
      gameId: Array.from(crypto.getRandomValues(new Uint8Array(16)), n => n.toString(16).padStart(2,"0")).join(""),
      size:fork?.size || 9, startPly, maxMoves:startPly + $("max-moves").valueAsNumber,
      moves:[...(fork?.moves || [])], calls:{black:0, white:0}, pending:null, result:null, kinds,
    };
    const controller = new AbortController();
    running = controller;
    clearKeys();
    for (const color of colors) {
      controls[color].disabled = true;
      $(color + "-name").textContent = seats[color].model || cpuNames[seats[color].style] || seatNames[kinds[color]];
    }
    $("limits").disabled = $("start").disabled = true;
    $("download").hidden = $("fresh").hidden = $("resume-note").hidden = true;
    $("stop").hidden = false;
    $("match-seats").hidden = $("board-state").hidden = $("run-state").hidden = false;
    $("status").textContent = "准备开局…";
    side = "";

    const run = async () => {
      if (resume && storage && checkpoint !== storage.getItem(saveKey(game.gameId))) {
        throw new Error("其他标签页已更新此局，请刷新后继续。");
      }
      const ref = await createReferee(game.maxMoves, {size:game.size, history:game.moves, startPly:game.startPly});
      controller.signal.throwIfAborted();
      if (ref.result()) throw new Error("这局棋已经结束。");
      record = game;
      record.seats = seats;
      if (!resume) checkpoint = null;
      rememberGame();
      display(ref.snapshot());
      callCount();
      if (matchMedia("(max-width: 760px)").matches) $("table").scrollIntoView({block:"start"});
      await playGame(ref, players, record, controller.signal, color => {
        side = color;
        if (players[color]) save();
        $("status").textContent = sides[color] + "正在思考…";
        callCount();
      }, async board => {
        save();
        display(board, record.moves.at(-1), true);
        if (!record.result) await new Promise(resolve => setTimeout(resolve, 300));
      });
      showPosition(ref);
    };
    try {
      if (navigator.locks) {
        await navigator.locks.request(saveKey(game.gameId), {ifAvailable:true}, async lock => {
          if (!lock) throw new Error("这局棋正在另一标签页进行。");
          await run();
        });
      } else await run();
    } catch (error) {
      if (controller.signal.aborted) $("status").textContent = "对局已停止";
      else {
        $("status").textContent = "对局中断";
        $("error").textContent = (side ? sides[side] + " · " : "") + error.message;
        $("error").hidden = false;
      }
    } finally {
      running = null;
      $("start").disabled = false;
      $("stop").hidden = true;
      $("download").hidden = !record;
      refreshForm();
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
