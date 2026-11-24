import { renderBoard } from "./board.js";

const $ = (id) => document.getElementById(id);
const columns = "ABCDEFGHJKLMNOPQRST";
const kindNames = { builtin: "内置程序", external: "外部 Agent" };
const faultNames = {
  timeout: "超时",
  protocol: "协议错误",
  "illegal-move": "非法落子",
  process: "进程异常",
  startup: "启动失败",
};
let report,
  active,
  ply = 0,
  previous,
  timer,
  loading = false;

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function validateReport(value) {
  if (
    value?.schemaVersion !== 1 ||
    !value.config ||
    !Array.isArray(value.players) ||
    !Array.isArray(value.standings) ||
    !Array.isArray(value.games)
  )
    throw new Error("运行报告格式不受支持。请查看原始报告或更新页面。");
  const players = new Set(value.players.map((player) => player?.id));
  if (
    players.size !== value.players.length ||
    value.players.some(
      (player) =>
        !player ||
        typeof player.id !== "string" ||
        !player.id ||
        typeof player.name !== "string" ||
        !player.name,
    )
  )
    throw new Error("参赛者信息不完整。");
  if (
    value.standings.some(
      (row) =>
        !row ||
        !players.has(row.playerID) ||
        [row.played, row.wins, row.losses, row.draws, row.points].some(
          (number) => !Number.isFinite(number) || number < 0,
        ),
    )
  )
    throw new Error("积分记录不完整。");
  const ids = new Set();
  for (const game of value.games) {
    if (
      !game ||
      typeof game.id !== "string" ||
      !game.id ||
      ids.has(game.id) ||
      !players.has(game.black) ||
      !players.has(game.white) ||
      ![9, 13, 19].includes(game.size) ||
      !Array.isArray(game.moves) ||
      !Array.isArray(game.frames) ||
      game.frames.length !== game.moves.length + 1 ||
      !game.result ||
      !["passes", "resign", "forfeit", "move-cap"].includes(game.result.reason) ||
      (game.result.winner && !players.has(game.result.winner))
    )
      throw new Error("对局记录不完整，无法可靠回放。");
    ids.add(game.id);
    for (const move of game.moves) {
      if (
        !move ||
        !Number.isInteger(move.x) ||
        !Number.isInteger(move.y) ||
        !(
          (move.x === -1 && move.y === -1) ||
          (move.x >= 0 && move.x < game.size && move.y >= 0 && move.y < game.size)
        )
      )
        throw new Error("棋谱坐标无效，无法可靠回放。");
    }
    for (const frame of game.frames) {
      if (
        !frame ||
        typeof frame.board !== "string" ||
        frame.board.length !== game.size ** 2 ||
        /[^012]/.test(frame.board) ||
        !frame.captures ||
        !Number.isInteger(frame.captures.black) ||
        !Number.isInteger(frame.captures.white) ||
        frame.captures.black < 0 ||
        frame.captures.white < 0
      )
        throw new Error("棋谱盘面不完整，无法可靠回放。");
    }
  }
  return value;
}

function playerName(id, data = report) {
  return data.players.find((player) => player.id === id)?.name || id;
}
function scoreText(number) {
  return Number.isFinite(number)
    ? new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(number)
    : "—";
}
function outcome(game, data = report) {
  const result = game.result;
  if (!result.winner) return "和棋";
  const name = playerName(result.winner, data);
  if (result.reason === "forfeit") return `${name} 判胜`;
  if (result.reason === "resign") return `${name} 中盘胜`;
  return result.score ? `${name} 胜 ${scoreText(result.score.margin)} 目` : `${name} 胜`;
}
function reasonText(game) {
  if (game.result.reason === "forfeit")
    return `${game.result.faultSide === 1 ? "黑方" : game.result.faultSide === 2 ? "白方" : "对手"}${faultNames[game.result.fault] || "违规"}，判负结束。`;
  if (game.result.reason === "resign") return "一方认输，本局未按盘面计分。";
  if (game.result.reason === "move-cap") return "达到手数上限，按盘面估分结束；未自动判定死子。";
  return "双方连续停一手，按盘面估分结束；未自动判定死子。";
}
function frameGame(game, position) {
  const n = Math.max(0, Math.min(game.moves.length, Math.trunc(position) || 0));
  const frame = game.frames[n];
  const last = game.moves[n - 1];
  return {
    size: game.size,
    board: Array.from(frame.board, Number),
    moves: game.moves.slice(0, n),
    next: frame.next,
    lastMove: frame.lastMove ?? (last?.x >= 0 ? last : null),
    captures: frame.captures,
    over: frame.over,
    score: frame.score,
  };
}
function replayLocation(hash, games) {
  const params = new URLSearchParams(hash.replace(/^#/, ""));
  const id = params.get("game");
  if (!id) return null;
  const game = games.find((game) => game.id === id);
  if (!game) return { missing: true };
  const value = params.get("ply");
  const n = value !== null && /^\d+$/.test(value) ? Number(value) : 0;
  return { game, ply: Math.min(game.moves.length, Number.isSafeInteger(n) ? n : 0) };
}
function gameHash(game, position) {
  return `#${new URLSearchParams({ game: game.id, ply: String(position) })}`;
}

function renderOverview() {
  $("run-title").textContent = report.config.title || "本轮公开运行";
  $("player-count").textContent = report.players.length;
  $("game-count").textContent = report.games.length;
  $("board-size").textContent = `${report.config.boardSize} 路`;
  const generated = new Date(report.generatedAt);
  $("generated-at").textContent = Number.isNaN(generated.getTime())
    ? "运行时间未提供"
    : new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(generated);
  if (!Number.isNaN(generated.getTime())) $("generated-at").dateTime = generated.toISOString();
  $("standings-body").replaceChildren();
  for (const standing of report.standings) {
    const row = element("tr");
    const name = element("td", "", playerName(standing.playerID));
    const notes = [`${standing.played} 局`];
    if (standing.forfeits) notes.push(`${standing.forfeits} 局违规判负`);
    if (standing.moveCaps) notes.push(`${standing.moveCaps} 局上限估分`);
    name.append(element("span", "standing-note", notes.join(" · ")));
    row.append(
      name,
      element("td", "", `${standing.wins} / ${standing.draws} / ${standing.losses}`),
      element("td", "", scoreText(standing.points)),
    );
    $("standings-body").append(row);
  }
  if (!report.standings.length) {
    const row = element("tr");
    const cell = element("td", "empty-state", "本轮暂无积分记录。");
    cell.colSpan = 3;
    row.append(cell);
    $("standings-body").append(row);
  }
  $("match-list").replaceChildren();
  $("matches-count").textContent = `${report.games.length} 局 · 可逐手回放`;
  for (const [index, game] of report.games.entries()) {
    const row = element("a", "match-row");
    row.href = gameHash(game, 0);
    row.dataset.game = game.id;
    const title = element("div", "match-row-title");
    title.append(
      element("span", "", `${playerName(game.black)} / ${playerName(game.white)}`),
      element("span", "match-number", String(index + 1).padStart(2, "0")),
    );
    const detail = element("div", "match-row-detail");
    detail.append(
      element("span", "", outcome(game)),
      element("span", "", `${game.moves.length} 手 · 第 ${game.round} 轮`),
    );
    row.append(title, detail);
    row.setAttribute(
      "aria-label",
      `回放第 ${index + 1} 局，黑方 ${playerName(game.black)}，白方 ${playerName(game.white)}，${outcome(game)}`,
    );
    $("match-list").append(row);
  }
  if (!report.games.length) $("match-list").append(element("p", "empty-state", "本轮尚无已记录对局。"));
  $("competitor-list").replaceChildren();
  for (const player of report.players) {
    const card = element("article", "competitor-card");
    const heading = element("div", "competitor-top");
    heading.append(
      element("h3", "", player.name),
      element("span", "kind-label", kindNames[player.kind] || player.kind || "未标注类型"),
    );
    card.append(
      heading,
      element(
        "p",
        "",
        player.policy || (player.style ? `内置策略：${player.style}` : "本轮未提供策略说明。"),
      ),
      element("p", "competitor-version", `${player.id}${player.version ? ` · ${player.version}` : ""}`),
    );
    $("competitor-list").append(card);
  }
  if (!report.players.length) $("competitor-list").append(element("p", "empty-state", "本轮尚无参赛者。"));
  const config = report.config;
  $("schedule-detail").textContent =
    `${config.rounds} 轮赛程，${config.openings?.length || 0} 种预置开局。${config.pairedColors ? "同组对手交换黑白执色，开局按配置固定。" : "各局执色与开局按运行配置记录。"}`;
  $("rules-detail").textContent =
    `${config.boardSize} 路棋盘，白贴 ${scoreText(config.komi)} 目。每手时限 ${scoreText(config.turnTimeoutMS / 1000)} 秒，单局最多 ${config.maxMoves} 手。`;
  const provenance = {
    引擎版本: report.provenance?.engineVersion,
    "Go 版本": report.provenance?.goVersion,
    "配置与名单 SHA256": report.provenance?.manifestSHA256,
  };
  $("provenance-list").replaceChildren();
  for (const [label, value] of Object.entries(provenance))
    $("provenance-list").append(
      element("dt", "", label),
      element("dd", "", value === undefined || value === "" ? "未提供" : String(value)),
    );
}

function pause() {
  clearInterval(timer);
  timer = undefined;
  if (active) $("toggle").textContent = ply === active.moves.length ? "重播棋谱" : "播放棋谱";
  $("toggle").setAttribute("aria-pressed", "false");
}
function seek(position, animate = false, updateURL = true) {
  if (!active) return;
  ply = Math.max(0, Math.min(active.moves.length, Math.trunc(position) || 0));
  const game = frameGame(active, ply);
  renderBoard(game, previous, animate && previous && game.moves.length === previous.moves.length + 1);
  previous = game;
  $("seek").value = ply;
  $("ply").textContent = `${ply} / ${active.moves.length} 手`;
  $("first").disabled = $("prev").disabled = ply === 0;
  $("last").disabled = $("next").disabled = ply === active.moves.length;
  $("toggle").disabled = active.moves.length === 0;
  $("seek").disabled = active.moves.length === 0;
  const move = active.moves[ply - 1];
  const point = move?.x >= 0 ? `${columns[move.x]}${active.size - move.y}` : "停一手";
  const last = ply
    ? `${ply % 2 ? "黑" : "白"} · ${point}${ply <= active.openingPlies ? " · 预置开局" : ""}`
    : "开局 · 黑棋先行";
  $("move-detail").textContent = last;
  $("black-captures").textContent = game.captures.black;
  $("white-captures").textContent = game.captures.white;
  $("board").setAttribute(
    "aria-label",
    `${active.size} 路棋盘，第 ${ply} 手，共 ${active.moves.length} 手，${last}，黑提 ${game.captures.black} 子，白提 ${game.captures.white} 子。`,
  );
  $("result").textContent = `本局结果 · ${outcome(active)}`;
  $("result-detail").textContent = reasonText(active);
  $("game-link").href = gameHash(active, ply);
  if (updateURL) history.replaceState(null, "", gameHash(active, ply));
  if (ply === active.moves.length) pause();
  else if (!timer) $("toggle").textContent = "播放棋谱";
}
function selectGame(game, position) {
  pause();
  active = game;
  previous = null;
  $("black-name").textContent = playerName(game.black);
  $("white-name").textContent = playerName(game.white);
  $("game-label").textContent = `第 ${report.games.indexOf(game) + 1} 局 · ${game.size} 路棋盘`;
  $("seek").max = game.moves.length;
  $("board-loading").hidden = true;
  for (const link of document.querySelectorAll(".match-row")) {
    if (link.dataset.game === game.id) link.setAttribute("aria-current", "true");
    else link.removeAttribute("aria-current");
  }
  seek(position, false, false);
}
function toggle() {
  if (!active || !active.moves.length) return;
  if (timer) {
    pause();
    return;
  }
  if (ply === active.moves.length) seek(0);
  timer = setInterval(() => seek(ply + 1, true), 1100);
  $("toggle").textContent = "暂停播放";
  $("toggle").setAttribute("aria-pressed", "true");
}
function route(scroll = false) {
  if (!report?.games.length) return;
  const target = replayLocation(location.hash, report.games);
  $("navigation-note").hidden = !target?.missing;
  if (target?.missing) $("navigation-note").textContent = "未找到链接中的棋局，已显示本轮首局。";
  if (target?.game) selectGame(target.game, target.ply);
  else if (target?.missing) selectGame(report.games[0], 0);
  else if (!active) {
    const game =
      report.games.find((game) => game.moves.length >= 20 && game.result.reason !== "forfeit") ||
      report.games.find((game) => game.moves.length > 0) ||
      report.games[0];
    selectGame(game, Math.floor(game.moves.length * 0.6));
  }
  if (scroll && target)
    $("replay").scrollIntoView({
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
      block: "start",
    });
}

async function loadReport() {
  if (loading) return;
  loading = true;
  $("data-error").hidden = true;
  $("retry").disabled = true;
  $("arena-content").setAttribute("aria-busy", "true");
  try {
    const response = await fetch("./report.json", { cache: "no-cache", signal: AbortSignal.timeout(10000) });
    if (!response.ok) throw new Error("运行报告暂时无法读取，请稍后重试。");
    report = validateReport(await response.json());
    renderOverview();
    route();
    if (!report.games.length) $("board-loading").textContent = "本轮尚无已记录对局。";
  } catch (error) {
    $("error-message").textContent =
      error.name === "TimeoutError"
        ? "读取战报超时，请重试。"
        : error instanceof SyntaxError
          ? "运行报告不是有效的 JSON，请查看原始报告。"
          : error.message || "运行报告暂时无法读取。";
    $("data-error").hidden = false;
    $("board-loading").textContent = "战报尚未载入";
  } finally {
    loading = false;
    $("retry").disabled = false;
    $("arena-content").setAttribute("aria-busy", "false");
  }
}

function start() {
  $("retry").addEventListener("click", loadReport);
  $("first").addEventListener("click", () => {
    pause();
    seek(0);
  });
  $("prev").addEventListener("click", () => {
    pause();
    seek(ply - 1);
  });
  $("next").addEventListener("click", () => {
    pause();
    seek(ply + 1, true);
  });
  $("last").addEventListener("click", () => {
    pause();
    seek(active.moves.length);
  });
  $("toggle").addEventListener("click", toggle);
  $("seek").addEventListener("input", (event) => {
    pause();
    seek(Number(event.target.value));
  });
  $("board").addEventListener("keydown", (event) => {
    if (
      !active ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      !["ArrowLeft", "ArrowRight", "Home", "End", " "].includes(event.key)
    )
      return;
    event.preventDefault();
    if (event.key === " ") {
      if (!event.repeat) toggle();
      return;
    }
    pause();
    seek(
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? active.moves.length
          : ply + (event.key === "ArrowRight" ? 1 : -1),
    );
  });
  addEventListener("hashchange", () => route(true));
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) pause();
  });
  $("copy-command").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText($("run-command").textContent);
      $("copy-note").textContent = "运行命令已复制。";
      $("copy-command").textContent = "已复制";
    } catch {
      $("copy-note").textContent = "浏览器无法复制，请选中命令手动复制。";
      $("copy-command").textContent = "请手动复制";
    }
  });
  loadReport();
}

if (typeof document !== "undefined") start();
