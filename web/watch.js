import { renderBoard } from "./board.js";
import { exhibition } from "./exhibition.js";

const $ = id => document.getElementById(id);
const table = document.documentElement.dataset.standalone === "true" ? null : new URLSearchParams(location.search).get("table");
let previous;
function show(game, animate) {
  renderBoard(game, previous, animate);
  previous = game;
  $("ply").textContent = `${game.size} 路 · 第 ${game.moves.length} 手`;
  const move = game.moves.at(-1);
  const color = game.moves.length % 2 ? "黑" : "白";
  const coord = move?.x >= 0 ? "ABCDEFGHJKLMNOPQRST"[move.x] + (game.size - move.y) : "停一手";
  $("watch-detail").textContent = move ? `${color} · ${coord}　黑提 ${game.captures.black} / 白提 ${game.captures.white}` : "黑棋先行";
  $("board").setAttribute("aria-label", $("ply").textContent + "，" + $("watch-detail").textContent);
}
function outcome(g) {
  if (!g.over) return "轮到" + (g.next === 1 ? "黑" : "白") + "棋";
  const winner = g.score.winner === 1 ? "黑" : "白";
  return g.resigned ? winner + "棋中盘胜" : winner + "棋胜 " + g.score.margin + " 目";
}
async function watchLive() {
  $("watch-mode").textContent = "实时观战";
  if (!/^[A-Za-z0-9_-]+$/.test(table)) { $("watch-status").textContent = "棋桌地址无效"; return; }
  let revision = -1;
  async function refresh() {
    try {
      const response = await fetch("./api/tables/" + table, { cache:"no-store",signal:AbortSignal.timeout(7000) });
      if (response.status === 404) { $("watch-status").textContent = "棋桌已关闭或不存在"; return; }
      if (!response.ok) throw Error("connection");
      const state = await response.json();
      if (state.revision !== revision) {
        const delta = state.game.moves.length - (previous?.moves.length ?? 0);
        show(state.game, !!previous && delta > 0 && delta <= 2);
        revision = state.revision;
      }
      $("players").textContent = state.opponent === "agent" ? "Agent 对 Agent" : "Agent 对 CPU · " + state.opponent;
      $("watch-status").textContent = outcome(state.game);
      if (state.game.over) return;
    } catch { $("watch-status").textContent = "连接中断，正在重连…"; }
    setTimeout(refresh, 850);
  }
  await refresh();
}

function watchReplay() {
  // Saved board snapshots come from Go. Seeking needs no second implementation of the rules.
  const {size,moves,frames,title} = exhibition;
  let i=0, timer;
  $("players").textContent = title;
  $("replay-controls").hidden = false;
  $("seek").max = frames.length-1;
  function pause() { clearInterval(timer); timer = undefined; $("toggle").textContent = i === frames.length-1 ? "重播" : "播放棋谱"; }
  function seek(n, animate) {
    i=n;
    const frame=frames[i], last=moves[i-1];
    const game={size,moves:moves.slice(0,i),board:Array.from(frame.board,Number),lastMove:last?.x>=0?last:null,captures:frame.captures};
    show(game,animate);
    $("seek").value=i;
    $("prev").disabled=i===0; $("next").disabled=i===frames.length-1;
    const finished = i === frames.length-1;
    $("watch-status").hidden = !finished;
    $("watch-status").textContent = finished ? exhibition.result : "";
    if(i===frames.length-1) pause();
  }
  function start() {
    if (i===frames.length-1) seek(0,false);
    timer=setInterval(()=>seek(i+1,true),1250);
    $("toggle").textContent="暂停";
  }
  $("prev").onclick=()=>{ pause(); seek(i-1,false); };
  $("next").onclick=()=>{ pause(); seek(i+1,true); };
  $("seek").oninput=e=>{ pause(); seek(Number(e.target.value),false); };
  $("toggle").onclick=()=>timer ? pause() : start();
  seek(0,false);
  if (!matchMedia("(prefers-reduced-motion: reduce)").matches) start();
}
if (table) watchLive(); else watchReplay();
