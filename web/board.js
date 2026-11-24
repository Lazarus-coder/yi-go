// Coordinates are always on the board plane. Lift affects only the drawing.
const svgNS = "http://www.w3.org/2000/svg";
const letters = "ABCDEFGHJKLMNOPQRST";
function svg(tag, attributes) {
  const el = document.createElementNS(svgNS, tag);
  for (const [key, value] of Object.entries(attributes)) el.setAttribute(key, value);
  return el;
}

const paint = `
  <radialGradient id="black-stone" cx=".34" cy=".23" r=".78">
    <stop stop-color="#888781"/><stop offset=".2" stop-color="#51514d"/>
    <stop offset=".52" stop-color="#292b2a"/><stop offset=".82" stop-color="#171a19"/>
    <stop offset=".94" stop-color="#101412"/><stop offset="1" stop-color="#373935"/>
  </radialGradient>
  <radialGradient id="white-stone" cx=".32" cy=".20" r=".83">
    <stop stop-color="#fffef9"/><stop offset=".42" stop-color="#f5f2e7"/>
    <stop offset=".77" stop-color="#ded8c7"/><stop offset=".94" stop-color="#b9b4a6"/>
    <stop offset="1" stop-color="#d6cfb9"/>
  </radialGradient>
  <radialGradient id="contact-shadow">
    <stop offset=".43" stop-color="#291602" stop-opacity=".75"/>
    <stop offset=".75" stop-color="#301b07" stop-opacity=".46"/>
    <stop offset="1" stop-color="#301b07" stop-opacity="0"/>
  </radialGradient>`;

function drawGrid(grid, size, step) {
  if (grid.dataset.size === String(size)) return;
  grid.dataset.size = size;
  grid.replaceChildren();
  for (let i = 0; i < size; i++) {
    const p = 48 + i * step;
    grid.append(svg("path", { d: `M48 ${p}H592 M${p} 48V592`, class: i === 0 || i === size - 1 ? "grid-edge" : "grid-line" }));
    for (const [x, y, text] of [[p,28,letters[i]], [p,620,letters[i]], [25,p+4,size-i], [615,p+4,size-i]]) {
      const label = svg("text", { x, y, class: "grid-label" });
      label.textContent = text;
      grid.append(label);
    }
  }
  const points = size === 9 ? [2,6] : size === 13 ? [3,9] : [3,9,15];
  for (const x of points) for (const y of points) grid.append(svg("circle",{cx:48+x*step,cy:48+y*step,r:2.6,class:"star-point"}));
  if (size !== 19) grid.append(svg("circle",{cx:320,cy:320,r:2.6,class:"star-point"}));
}

// Sample gravity and inelastic impacts once. The browser interpolates frames.
function dropFrames() {
  const frames = [];
  let height = 1, velocity = 0;
  for (let i = 0; i < 72; i++) {
    frames.push(height);
    velocity -= 18 / 120;
    height += velocity / 120;
    if (height <= 0) { height = 0; velocity *= -0.26; }
  }
  frames.push(0);
  return frames;
}
const bounce = dropFrames();
const dropDuration = 500;
const impactAt = dropDuration * bounce.indexOf(0) / (bounce.length - 1);
const betweenMoves = 300;
const reduced = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

function stoneNode(color, x, y, r, marked) {
  const base = svg("g", { transform: `translate(${x} ${y})` });
  // Contact shadows use a soft radial falloff; a large board needs no SVG filters.
  const shadow = svg("ellipse", { cx:r*.12, cy:r*.21, rx:r*1.18, ry:r*1.05, fill:"url(#contact-shadow)" });
  const body = svg("g", {});
  const face = svg("circle", { r, fill: color === 1 ? "url(#black-stone)" : "url(#white-stone)", stroke:color === 1 ? "#161915" : "#b2aa97", "stroke-width":r*.026 });
  body.append(face);
  if (color === 2) {
    // Fine shell striations, each stone cut at a slightly different angle.
    const angle = (Math.round(x + y) % 30) - 15;
    body.append(svg("path",{d:`M${-.68*r} ${-.52*r} Q${-.2*r} ${-.3*r} ${.62*r} ${-.54*r} M${-.8*r} ${-.35*r} Q0 ${-.09*r} ${.76*r} ${-.35*r}`,transform:`rotate(${angle})`,fill:"none",stroke:"#a69679","stroke-opacity":.15,"stroke-width":r*.028}));
  }
  if (marked) body.append(svg("circle",{ r:r*.155,fill:"none",stroke:color===1?"#f2d6a9":"#a14c30","stroke-width":Math.max(1.25,r*.073) }));
  base.append(shadow,body);
  return {base,shadow,body};
}

function drop(node,r,delay) {
  const options = {duration:dropDuration,delay,fill:"backwards"};
  node.body.animate(bounce.map(h=>({transform:`translate(${-h*r*.18}px,${-h*r*1.32}px) scale(${1+h*.08})`,opacity:1-h})),options);
  node.shadow.animate(bounce.map(h=>({transform:`translate(${h*r*.38}px,${h*r*.35}px) scale(${1+h*.19})`,opacity:1-h*.66})),options);
}

function lift(node,r,delay,dx) {
  const animation = node.body.animate([
    {transform:"translate(0,0) scale(1)",opacity:1},
    {transform:`translate(${dx*.25}px,${-r*.8}px) scale(1.08)`,opacity:.94,offset:.38},
    {transform:`translate(${dx}px,${-r*1.65}px) scale(.85)`,opacity:0}
  ],{duration:280,delay,easing:"ease-in",fill:"forwards"});
  node.shadow.animate([{opacity:1},{opacity:0}],{duration:260,delay,fill:"forwards"});
  animation.onfinish=()=>node.base.remove();
}

export function renderBoard(game,previous=null,animateTurn=false) {
  const defs = document.querySelector("#board defs");
  if (!defs.hasChildNodes()) defs.innerHTML = paint;
  const size=game.size, step=544/(size-1), r=step*.467;
  drawGrid(document.getElementById("grid"),size,step);
  const stones=document.getElementById("stones");
  stones.replaceChildren(); // A sync, undo or new turn supersedes any pending animation.
  const motion=animateTurn && !reduced() && previous?.size===size && typeof stones.animate==="function";
  const justPlayed=motion ? game.moves.slice(previous.moves.length) : [];
  function orderAt(point,color) {
    return justPlayed.findIndex((move,i)=>move.x===point%size && move.y===Math.floor(point/size) && (previous.moves.length+i)%2+1===color);
  }
  for(let point=0;point<game.board.length;point++) {
    const color=game.board[point], x=point%size, y=Math.floor(point/size);
    const make=(c,last=false)=>stoneNode(c,48+x*step,48+y*step,r,last);
    if(color) {
      const node=make(color,game.lastMove?.x===x && game.lastMove?.y===y);
      stones.append(node.base);
      const ply=orderAt(point,color);
      if(ply>=0) drop(node,r,ply*betweenMoves);
    }
    if(!motion) continue;
    // Polling can deliver a placement and its capture in the same update.
    let removed=previous.board[point];
    let transient=false;
    const firstColor = previous.moves.length % 2 + 1;
    if (!removed && !color && orderAt(point, firstColor) === 0) {
      removed = firstColor;
      transient = true;
    }
    if(removed && removed!==color) {
      const node=make(removed);
      stones.append(node.base);
      if(transient) drop(node,r,0);
      const capturePly=justPlayed.findIndex((_,i)=>(previous.moves.length+i)%2+1!==removed);
      lift(node,r,Math.max(0,capturePly)*betweenMoves+impactAt,(x<(size-1)/2?-1:1)*r*.85);
    }
  }
}
