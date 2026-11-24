import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";
import { forkLink, readFork } from "../replay-fork.js";

const source = (await readFile(new URL("./arena.js", import.meta.url), "utf8")).replace(/^import .*\n/gm, "");
const hooks =
  "\nglobalThis.api = { validateReport, frameGame, replayLocation, gameHash, outcome, reasonText };";
const pure = vm.runInNewContext(source + hooks + "\napi", { URLSearchParams, Intl });
function fixture() {
  const moves = [
    { x: 3, y: 5 },
    { x: 4, y: 5 },
    { x: 3, y: 4 },
  ];
  const board = Array(81).fill(0);
  const frames = [
    { board: board.join(""), next: 1, captures: { black: 0, white: 0 }, lastMove: null, over: false },
  ];
  for (const [index, move] of moves.entries()) {
    board[move.y * 9 + move.x] = (index % 2) + 1;
    frames.push({
      board: board.join(""),
      next: index % 2 ? 1 : 2,
      captures: { black: 0, white: 0 },
      lastMove: move,
      over: index === 2,
    });
  }
  return {
    schemaVersion: 1,
    generatedAt: "2026-11-24T00:00:00Z",
    config: {
      title: "Test run",
      boardSize: 9,
      komi: 6.5,
      turnTimeoutMS: 1000,
      maxMoves: 144,
      rounds: 1,
      pairedColors: true,
      openings: [{ id: "empty", name: "Empty", moves: [] }],
    },
    provenance: { engineVersion: "test", goVersion: "test", manifestSHA256: "test" },
    players: [
      { id: "a", name: "A", kind: "external" },
      { id: "b", name: "B", kind: "builtin", style: "china" },
    ],
    standings: [
      { playerID: "a", played: 1, wins: 1, losses: 0, draws: 0, points: 1 },
      { playerID: "b", played: 1, wins: 0, losses: 1, draws: 0, points: 0 },
    ],
    games: [
      {
        id: "game-1",
        openingID: "empty",
        round: 1,
        black: "a",
        white: "b",
        size: 9,
        komi: 6.5,
        openingPlies: 0,
        moves,
        frames,
        result: { winner: "a", winnerColor: 1, reason: "resign" },
        durationMS: 100,
      },
    ],
  };
}

test("report validation rejects corrupt replay frames and unknown participants", () => {
  const valid = fixture();
  assert.equal(pure.validateReport(valid), valid);
  for (const corrupt of [
    (value) => {
      value.schemaVersion = 2;
    },
    (value) => {
      value.players.push(value.players[0]);
    },
    (value) => {
      value.players[0] = null;
    },
    (value) => {
      value.standings[0].points = -1;
    },
    (value) => {
      value.games[0].white = "unknown";
    },
    (value) => {
      value.games[0].frames.pop();
    },
    (value) => {
      value.games[0].frames[1].board = "0";
    },
    (value) => {
      value.games[0].frames[1].board = "x".repeat(81);
    },
    (value) => {
      value.games[0].frames[1].captures.black = -1;
    },
    (value) => {
      value.games[0].moves[1].x = 9;
    },
    (value) => {
      value.games[0].moves[1] = { x: -1, y: 0 };
    },
  ]) {
    const value = fixture();
    corrupt(value);
    assert.throws(() => pure.validateReport(value));
  }
});

test("replay snapshots use the recorded board, captures and exact history prefix", () => {
  const game = fixture().games[0];
  const start = pure.frameGame(game, 0);
  assert.equal(start.moves.length, 0);
  assert.equal(start.board.filter(Boolean).length, 0);
  assert.equal(start.lastMove, null);
  const middle = pure.frameGame(game, 2);
  assert.equal(middle.moves.length, 2);
  assert.equal(middle.board[5 * 9 + 3], 1);
  assert.equal(middle.board[5 * 9 + 4], 2);
  assert.equal(middle.lastMove, game.moves[1]);
  assert.equal(pure.frameGame(game, 1000).moves.length, 3);
  assert.equal(pure.frameGame(game, -50).moves.length, 0);
  assert.equal(game.moves.length, 3, "seeking does not mutate the source record");
});

test("deep links preserve move zero, clamp extremes and reject unknown games", () => {
  const games = fixture().games;
  assert.equal(pure.replayLocation("#game=game-1&ply=0", games).ply, 0);
  assert.equal(pure.replayLocation("#game=game-1&ply=999", games).ply, 3);
  assert.equal(pure.replayLocation("#game=game-1&ply=-1", games).ply, 0);
  assert.equal(pure.replayLocation("#game=missing&ply=1", games).missing, true);
  assert.equal(pure.replayLocation("#standings", games), null);
  games[0].id = "a & b";
  const location = pure.replayLocation(pure.gameHash(games[0], 2), games);
  assert.equal(location.game, games[0]);
  assert.equal(location.ply, 2);
});

test("forfeits and resignation never invent a points margin", () => {
  const report = fixture(),
    game = report.games[0];
  game.result.score = { margin: 100 };
  assert.equal(pure.outcome(game, report), "A 中盘胜");
  game.result.reason = "forfeit";
  game.result.fault = "timeout";
  game.result.faultSide = 2;
  assert.equal(pure.outcome(game, report), "A 判胜");
  assert.match(pure.reasonText(game), /白方超时/);
  game.result.reason = "move-cap";
  game.result.score.margin = 2.5;
  assert.equal(pure.outcome(game, report), "A 胜 2.5 目");
  assert.match(pure.reasonText(game), /手数上限/);
});

function domHarness(data, failFirst = false, hash = "", search = "") {
  const nodes = new Map(),
    events = {},
    intervals = new Map(),
    draws = [];
  let intervalID = 0;
  function node(tag = "div") {
    return {
      tagName: tag,
      children: [],
      attributes: {},
      dataset: {},
      handlers: {},
      textContent: "",
      value: "",
      disabled: false,
      hidden: false,
      files: [],
      focus() { this.focused = true; },
      append(...children) {
        this.children.push(...children);
      },
      replaceChildren(...children) {
        this.children = children;
      },
      setAttribute(name, value) {
        this.attributes[name] = String(value);
      },
      removeAttribute(name) {
        delete this.attributes[name];
      },
      addEventListener(name, fn) {
        this.handlers[name] = fn;
      },
      scrollIntoView() { this.scrolls = (this.scrolls || 0) + 1; },
    };
  }
  const get = (id) => {
    if (!nodes.has(id)) nodes.set(id, node());
    return nodes.get(id);
  };
  const context = {
    document: {
      getElementById: get,
      createElement: node,
      querySelectorAll: () => get("match-list").children,
      addEventListener(name, fn) {
        events[name] = fn;
      },
    },
    renderBoard: (...args) => draws.push(args),
    forkLink,
    URLSearchParams,
    Intl,
    AbortSignal: { timeout() {} },
    fetch: async () => {
      if (failFirst) {
        failFirst = false;
        throw Error("测试网络中断");
      }
      return { ok: true, json: async () => data };
    },
    location: { hash, search },
    history: {
      replaceState(a, b, value) {
        context.location.hash = value;
      },
    },
    matchMedia: () => ({ matches: true }),
    setInterval(fn) {
      intervals.set(++intervalID, fn);
      return intervalID;
    },
    clearInterval(id) {
      intervals.delete(id);
    },
    addEventListener(name, fn) {
      events[name] = fn;
    },
  };
  vm.runInNewContext(source, context);
  return { nodes, draws, intervals, context, events };
}
const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve();
};

test("controls, keyboard, deep links and end-of-game replay keep state in sync", async () => {
  const app = domHarness(fixture(), false, "#game=game-1&ply=0");
  await flush();
  assert.equal(app.nodes.get("ply").textContent, "0 / 3 手");
  assert.equal(app.intervals.size, 0, "the arena never autoplays on load");
  assert.equal(app.nodes.get("first").disabled, true);
  assert.equal(app.nodes.get("data-error").hidden, true);
  app.nodes.get("next").handlers.click();
  assert.equal(app.nodes.get("ply").textContent, "1 / 3 手");
  assert.equal(app.draws.at(-1)[0].board[48], 1);
  assert.match(app.context.location.hash, /ply=1/);
  app.nodes.get("board").handlers.keydown({ key: "End", preventDefault() {} });
  assert.equal(app.nodes.get("next").disabled, true);
  assert.equal(app.nodes.get("toggle").textContent, "重播");
  app.nodes.get("toggle").handlers.click();
  assert.equal(app.nodes.get("ply").textContent, "0 / 3 手");
  assert.equal(app.intervals.size, 1);
  const advance = [...app.intervals.values()][0];
  advance();
  advance();
  advance();
  assert.equal(app.intervals.size, 0);
  assert.equal(app.nodes.get("ply").textContent, "3 / 3 手");
  app.nodes.get("board").handlers.keydown({ key: "Home", preventDefault() {} });
  assert.equal(app.nodes.get("ply").textContent, "0 / 3 手");
  app.context.location.hash = "#game=game-1&ply=2";
  app.events.hashchange();
  assert.equal(app.nodes.get("ply").textContent, "2 / 3 手");
});

test("selecting the same game again brings the replay back into view", async () => {
  const app = domHarness(fixture(), false, "#game=game-1&ply=0");
  await flush();
  let prevented = false;
  app.nodes.get("match-list").handlers.click({
    button: 0, target: {closest: () => ({hash: app.context.location.hash})},
    preventDefault() { prevented = true; },
  });
  assert.equal(prevented, true);
  assert.equal(app.nodes.get("replay").scrolls, 1);
  assert.equal(app.nodes.get("ply").textContent, "0 / 3 手");
});

test("fork URLs round-trip board sizes, coordinates, and passes, and reject malformed histories", () => {
  for (const size of [9, 13, 19]) {
    const url = forkLink(size, [{x:size-1,y:size-1}, {x:-1,y:-1}, {x:0,y:0}]);
    const lastColumn = {9:"J",13:"N",19:"T"}[size];
    assert.deepEqual(readFork(new URL(url, "https://example.com/yi/").hash),
      { size, moves:[lastColumn+"1","pass","A"+size] });
  }
  assert.deepEqual(readFork("#fork=9&moves="), {size:9, moves:[]});
  assert.equal(readFork("#game=some-game&ply=1"), null);
  for (const hash of ["#fork=10&moves=", "#fork=9", "#fork=9&moves=A1,I2", "#fork=13&moves=A14",
    "#fork=9&moves=K9", "#fork=9&moves=pass,", "#fork=9&moves=resign", "#fork=9&moves=A01",
    "#fork=9&fork=19&moves=", "#fork=9&moves=D4&moves=Q16", "#fork=9&moves="+Array(2001).fill("D1").join(",")]) {
    assert.throws(() => readFork(hash));
  }
});

test("published capture timeline leads to an exact fork before the 54-stone capture", async () => {
  const data = JSON.parse(await readFile(new URL("./report.json", import.meta.url), "utf8"));
  const app = domHarness(data, false, "#game=g-000001&ply=0");
  await flush();
  const nodes = app.nodes;
  const jump = nodes.get("capture-jump");
  const biggest = jump.children[1];
  assert.equal(biggest.textContent, "第 140 手 · 白提 54 子");
  assert.ok(nodes.get("capture-marks").children.some(tick => tick.value === 140));
  jump.handlers.change({ target:biggest });
  assert.match(nodes.get("move-detail").textContent, /白 · D1 · 提 54 子/);
  nodes.get("prev").handlers.click();
  const opening = readFork(new URL(nodes.get("fork-link").href, "https://example.com/yi/").hash);
  const published = data.games[0];
  assert.deepEqual(opening.moves, published.moves.slice(0,139).map(move =>
    move.x < 0 ? "pass" : "ABCDEFGHJ"[move.x]+(9-move.y)));
  nodes.get("last").handlers.click();
  assert.equal(nodes.get("fork-link").hidden, false, "a timed-off position can be continued");
  app.context.location.hash = "#game=g-000003&ply=108";
  app.events.hashchange();
  assert.equal(nodes.get("fork-link").hidden, true, "two passes ended this position");
  nodes.get("prev").handlers.click();
  assert.equal(nodes.get("fork-link").hidden, false);
});

test("file viewer opens a local report without fetching or linking it to a published game", async () => {
  const app = domHarness(fixture(), false, "", "?open=1");
  const nodes = app.nodes;
  const picker = nodes.get("report-file");
  assert.equal(picker.focused, true);
  assert.equal(nodes.get("league-details").open, true);
  assert.equal(nodes.get("arena-content").hidden, true);
  assert.equal(app.draws.length, 0);
  app.context.fetch = () => assert.fail("private file must stay in the browser");
  let read = false;
  picker.files = [{ size:129*1024*1024, text:() => { read = true; } }];
  picker.handlers.change();
  await flush();
  assert.match(nodes.get("error-message").textContent, /128 MiB/);
  assert.equal(read, false);
  const privateReport = fixture();
  privateReport.config.title = "Local agent run";
  picker.files = [new Blob([JSON.stringify(privateReport)])];
  picker.handlers.change();
  await flush();
  assert.equal(nodes.get("run-title").textContent, "Local agent run");
  assert.equal(nodes.get("game-link").hidden, true);
  assert.equal(nodes.get("published-links").hidden, true);
  assert.equal(nodes.get("arena-content").hidden, false);
  assert.equal(nodes.get("ply").textContent, "0 / 3 手");
  nodes.get("next").handlers.click();
  assert.deepEqual(readFork(new URL(nodes.get("fork-link").href, "https://example.com").hash).moves, ["D4"]);
});

test("failed report loading exposes retry and successful retry renders real data", async () => {
  const app = domHarness(fixture(), true);
  await flush();
  assert.equal(app.nodes.get("data-error").hidden, false);
  assert.equal(app.draws.length, 0, "failed loading must not fabricate a board");
  await app.nodes.get("retry").handlers.click();
  assert.equal(app.nodes.get("data-error").hidden, true);
  assert.equal(app.nodes.get("player-count").textContent, 2);
  assert.equal(app.nodes.get("game-count").textContent, 1);
  assert.equal(app.nodes.get("standings-body").children.length, 2);
  assert.equal(
    app.draws.at(-1)[0].board.filter(Boolean).length,
    1,
    "initial showcase uses a real recorded middle position",
  );
});

test("the published report satisfies the frontend contract", async (t) => {
  let report;
  try {
    report = JSON.parse(await readFile(new URL("./report.json", import.meta.url), "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") {
      t.skip("publish report.json after running the real benchmark");
      return;
    }
    throw error;
  }
  pure.validateReport(report);
  for (const game of report.games) {
    assert.equal(pure.frameGame(game, game.moves.length).board.length, game.size ** 2);
    assert.equal(typeof pure.outcome(game, report), "string");
  }
});
