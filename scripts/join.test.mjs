import test from "node:test";
import assert from "node:assert/strict";
import {callBudget, makeSGF, playGame} from "../web/join.js";

const color = index => index % 2 === 0 ? "black" : "white";
function game(kinds, maxMoves = 3) {
  const record = {gameId:"sample", kinds, maxMoves, moves:[], calls:{black:0, white:0}, result:null};
  const accepted = [];
  const ref = {
    turn:() => ({revision:accepted.length, color:color(accepted.length)}),
    cpu:async () => "pass",
    play:async move => { accepted.push(move); },
    snapshot:() => ({moves:[...accepted]}),
    result:() => accepted.length === maxMoves ? {winner:"white", reason:"move-cap", score:{margin:6.5}} : null,
  };
  return {record, ref, accepted};
}

test("each seat's maximum is known before an odd-length game", () => {
  assert.deepEqual(callBudget({black:"agent", white:"chat"}, 3), {black:2, white:1});
  assert.deepEqual(callBudget({black:"cpu", white:"chat"}, 3), {black:0, white:1});
});

test("two agents receive distinct turn IDs and exhaust only their own budgets", async () => {
  const {record, ref} = game({black:"agent", white:"chat"});
  const turns = [], snapshots = [];
  const player = {async choose(turn) { turns.push(turn); return "pass"; }};
  await playGame(ref, {black:player, white:player}, record, new AbortController().signal, () => {}, board => snapshots.push(board));
  assert.deepEqual(turns.map(t => [t.color, t.id, t.gameId, t.timeLimitMS]), [
    ["black","sample:0","sample",30000], ["white","sample:1","sample",30000], ["black","sample:2","sample",30000],
  ]);
  assert.deepEqual(record.calls, {black:2, white:1});
  assert.deepEqual(record.moves, ["pass","pass","pass"]);
  assert.equal(record.result.reason,"move-cap");
  assert.equal(snapshots.length,3);
});

test("a cancelled decision never reaches the referee or starts another request", async () => {
  const {record, ref, accepted} = game({black:"chat", white:"cpu"});
  const controller = new AbortController();
  const player = {async choose() { controller.abort(); return "D4"; }};
  await assert.rejects(playGame(ref, {black:player}, record, controller.signal, () => {}, () => {}), {name:"AbortError"});
  assert.deepEqual(record.calls, {black:1, white:0});
  assert.deepEqual(accepted, []);
  assert.equal(record.result,null);
});

test("a failed second seat keeps the first legal move for an unfinished SGF", async () => {
  const {record, ref} = game({black:"cpu", white:"agent"});
  await assert.rejects(playGame(ref, {white:{choose() { throw Error("offline"); }}}, record,
    new AbortController().signal, () => {}, () => {}), /offline/);
  assert.deepEqual(record.calls, {black:0, white:1});
  assert.deepEqual(record.moves, ["pass"]);
  assert.match(makeSGF(record), /Unfinished/);
  assert.doesNotMatch(makeSGF(record), /RE\[/);
});

test("standard SGF uses Go coordinates, distinct pass and resignation, and allowlisted fields", () => {
  const {record} = game({black:"chat", white:"agent"}, 80);
  Object.assign(record, {moves:["A9","J1","pass","resign"], key:"private-token", baseURL:"https://private.example",
    models:["private-name"], result:{winner:"black", reason:"resign"}});
  const sgf = makeSGF(record);
  assert.match(sgf, /PB\[BYOK model\]PW\[HTTP agent\]RE\[B\+R\]/);
  assert.match(sgf, /;B\[aa\];W\[ii\];B\[\]\)/);
  assert.doesNotMatch(sgf, /private|https:/);
  record.result = {winner:"white", reason:"passes", score:{margin:6.5}};
  assert.match(makeSGF(record), /RE\[W\+6.5\]/);
});
