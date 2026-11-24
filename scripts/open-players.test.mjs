import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {once} from "node:events";
import {createServer} from "node:http";
import {mkdtemp, readFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {setTimeout as pause} from "node:timers/promises";
import {createInterface} from "node:readline";
import test from "node:test";
import {createPlayer} from "../web/open-players.js";

const turn = {
  type:"turn", protocol:"yi-agent-v1", id:"g:1", gameId:"g", color:"black", revision:0,
  players:{black:"black",white:"white"}, size:9, komi:6.5, history:[], legal:["C3", "pass", "resign"],
  ascii:"X = black, O = white", board:Array(81).fill(0), captures:{black:0,white:0},
  rules:{ko:"simple",scoring:"area"}, maxMoves:16, remainingMoves:16, turnTimeoutMS:2000, timeLimitMS:2000,
};
const move = {type:"move", id:turn.id, revision:0, move:"C3"};
async function fixture(t, handle) {
  const server = createServer(async (req,res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    handle(req,res,JSON.parse(Buffer.concat(chunks).toString() || "null"));
  });
  server.listen(0,"127.0.0.1");
  await once(server, "listening");
  t.after(() => { server.closeAllConnections(); server.close(); });
  return "http://127.0.0.1:" + server.address().port;
}

test("BYOK and custom agent each get only their own credentials and one turn", async t => {
  const calls = [];
  const base = await fixture(t, (req,res,body) => {
    calls.push({path:req.url,auth:req.headers.authorization,body});
    res.end(JSON.stringify(req.url === "/turn" ? move : {choices:[{message:{content:'```json\n{"move":"pAsS"}\n```'}}]}));
  });
  const chat = createPlayer({kind:"chat",baseURL:base+"/v1/",model:"local",key:"chat-test-key"}, "black");
  const agent = createPlayer({kind:"agent",baseURL:base+"/turn",key:"my-agent-test-key"}, "black");
  assert.equal(await chat.choose(turn), "pass");
  assert.equal(await agent.choose({...turn, key:"untouched-test-key"}), "C3");
  assert.equal(calls.length,2);
  assert.deepEqual(calls.map(c => [c.path,c.auth]), [["/v1/chat/completions","Bearer chat-test-key"],["/turn","Bearer my-agent-test-key"]]);
  assert.equal(calls[0].body.model,"local");
  assert.equal(calls[0].body.max_tokens,256);
  assert.deepEqual(JSON.parse(calls[0].body.messages[1].content).legal,turn.legal);
  assert.deepEqual(calls[1].body, turn);
  assert.ok(calls.every(call => !JSON.stringify(call.body).includes("test-key")));
});

test("rejects unsafe addresses before a request and never follows a redirect", async t => {
  for (const baseURL of ["http://evil.invalid","http://127.99.0.1/turn","http://[::1]/turn","file:///tmp/agent","https://u:p@host","https://host/v1?k=test","https://host/#key"]) {
    assert.throws(() => createPlayer({kind:"agent",baseURL}, "black"));
  }
  let contacts = 0;
  const target = await fixture(t,(req,res) => { contacts++; res.end("{}"); });
  const source = await fixture(t,(req,res) => { res.writeHead(307,{Location:target+"/elsewhere"}); res.end(); });
  await assert.rejects(createPlayer({kind:"chat",baseURL:source,model:"local",key:"abc"}, "black").choose(turn), /无法连接/);
  assert.equal(contacts,0);
});

test("sanitizes upstream failures and rejects stale, oversized or illegal responses", async t => {
  const base = await fixture(t,(req,res) => {
    if (req.url === "/secret") { res.writeHead(401); return res.end("PRIVATE CONTENT"); }
    if (req.url === "/large") { res.writeHead(200); res.write("a".repeat(60000)); return res.end("a".repeat(60000)); }
    res.end(JSON.stringify(req.url === "/late" ? {...move,id:"yesterday"} : {...move,move:"Z1"}));
  });
  for (const [path,expected] of [["secret",/HTTP 401/],["large",/64 KiB/],["late",/编号/],["illegal",/合法/]]) {
    const seat = createPlayer({kind:"agent",baseURL:base+"/"+path},"black");
    await assert.rejects(seat.choose(turn), e => expected.test(e.message) && !/PRIVATE|127\.0\.0\.1/.test(e.message));
  }
});

test("canceling or timing out a slow provider performs no retry", async t => {
  let attempts = 0, received;
  const arrived = new Promise(resolve => {received = resolve;});
  const base = await fixture(t,() => { attempts++; received(); });
  const seat = createPlayer({kind:"chat",baseURL:base,model:"test"},"black");
  const ctrl = new AbortController();
  const request = seat.choose(turn,ctrl.signal);
  await arrived;
  ctrl.abort();
  await assert.rejects(request, {name:"AbortError"});
  await assert.rejects(seat.choose({...turn,timeLimitMS:100}), /超时/);
  assert.equal(attempts,2);
});

test("the loopback bridge gates origins and tokens before running the real JSONL agent", {skip:process.platform==="win32", timeout:10000}, async t => {
  const origin = "https://lazarus-coder.github.io";
  const processAgent = spawn("python3", ["examples/http/bridge.py","--port","0","--token-env","YI_HTTP_FIXTURE","--","python3","examples/agent/agent.py","7"], {
    cwd:new URL("../",import.meta.url), env:{...process.env,YI_HTTP_FIXTURE:"bridge-test"}, stdio:["ignore","pipe","pipe"],
  });
  const closed = once(processAgent,"close");
  t.after(async () => { processAgent.kill(); await closed; });
  const lines=createInterface({input:processAgent.stdout});
  const [line] = await once(lines,"line");
  const url = line.match(/http:\/\/127\.0\.0\.1:\d+\/turn$/)[0];
  const headers={origin,authorization:"Bearer bridge-test","Content-Type":"application/json"};
  const preflight = await fetch(url,{method:"OPTIONS",headers:{origin,"Access-Control-Request-Method":"POST","Access-Control-Request-Private-Network":"true"}});
  assert.equal(preflight.status,204);
  assert.equal(preflight.headers.get("access-control-allow-origin"),origin);
  assert.equal(preflight.headers.get("access-control-allow-private-network"),"true");
  const post=(opts,event=turn)=>fetch(url,{method:"POST",headers:{...headers,...opts},body:JSON.stringify(event)});
  const denied=await post({origin:"https://elsewhere.invalid"});
  assert.equal(denied.status,403);
  assert.equal(denied.headers.get("access-control-allow-origin"),null);
  assert.equal((await post({authorization:"Bearer invalid"})).status,401);
  assert.equal((await post({},{...turn,history:"bad"})).status,400);
  assert.equal((await post({},{...turn,ascii:"x".repeat(66000)})).status,413);
  const response = await post({});
  assert.equal(response.status,200);
  assert.deepEqual(await response.json(),move);
});

test("disconnecting terminates the running JSONL process and keeps its bridge token private", {skip:process.platform==="win32", timeout:8000}, async t => {
  const directory = await mkdtemp(join(tmpdir(),"yi-http-"));
  t.after(() => rm(directory,{recursive:true,force:true}));
  const marker = join(directory,"pid");
  const child = [
    "import sys, json, os, time",
    "assert 'YI_HTTP_FIXTURE' not in os.environ",
    "hello = json.loads(input())",
    "print(json.dumps(dict(type='ready',protocol=hello['protocol'],id=hello['id'])), flush=True)",
    "turn = json.loads(input())",
    "open(sys.argv[1], 'w').write(str(os.getpid()))",
    "time.sleep(10)",
  ].join("\n");
  const bridge = spawn("python3", ["examples/http/bridge.py","--port","0","--token-env","YI_HTTP_FIXTURE","--","python3","-c",child,marker], {
    cwd:new URL("../",import.meta.url), env:{...process.env,YI_HTTP_FIXTURE:"bridge-test"}, stdio:["ignore","pipe","pipe"],
  });
  const closed = once(bridge,"close");
  t.after(async () => { bridge.kill(); await closed; });
  const lines = createInterface({input:bridge.stdout});
  const [line] = await once(lines,"line");
  const url = line.match(/http:\/\/127\.0\.0\.1:\d+\/turn$/)[0];
  const ctrl = new AbortController();
  const request = fetch(url,{method:"POST",headers:{origin:"https://lazarus-coder.github.io",authorization:"Bearer bridge-test","Content-Type":"application/json"},
    body:JSON.stringify({...turn,turnTimeoutMS:30000,timeLimitMS:30000}),signal:ctrl.signal});
  let pid;
  for (let i=0; !pid && i<100; i++) {
    pid = Number(await readFile(marker,"utf8").catch(()=>""));
    if (!pid) await pause(15);
  }
  ctrl.abort();
  await assert.rejects(request, {name:"AbortError"});
  assert.ok(pid>0, "child started with token stripped");
  for (let i=0; i<100; i++) {
    try { process.kill(pid,0); } catch (error) { assert.equal(error.code,"ESRCH"); return; }
    await pause(15);
  }
  assert.fail("canceled child is still running");
});
