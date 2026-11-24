// Build dist first, then run: node scripts/build-html.mjs
import { readFile, writeFile } from "node:fs/promises";
import { dirname, extname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const directory = resolve(dirname(fileURLToPath(import.meta.url)), "../dist");
const modules = {};
const styles = new Map();
const mimeTypes = { ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".svg": "image/svg+xml", ".webp": "image/webp", ".woff2": "font/woff2", ".wasm": "application/wasm", ".js": "text/javascript" };

function localPath(reference, parent = directory) {
  if (/^(?:[a-z]+:|\/\/|\/)/i.test(reference)) throw new Error(`Expected a local asset: ${reference}`);
  const path = resolve(parent, reference);
  if (!path.startsWith(directory + sep)) throw new Error(`Asset escapes dist: ${reference}`);
  return path;
}

async function dataURL(path) {
  const mime = mimeTypes[extname(path)];
  if (!mime) throw new Error(`Unsupported embedded asset: ${path}`);
  return `data:${mime};base64,${(await readFile(path)).toString("base64")}`;
}

async function stylesheet(path) {
  if (styles.has(path)) return;
  let css = await readFile(path, "utf8");
  if (/@import\b/i.test(css)) throw new Error(`Inline imported styles before bundling: ${path}`);
  for (const match of [...css.matchAll(/url\(\s*(?:"([^"]+)"|'([^']+)'|([^\s)]+))\s*\)/g)]) {
    const reference = match[1] || match[2] || match[3];
    if (reference.startsWith("data:") || reference.startsWith("#")) continue;
    const url = await dataURL(localPath(reference, dirname(path)));
    css = css.replace(match[0], `url("${url}")`);
  }
  if (/<\/style/i.test(css)) throw new Error(`Unsafe closing style tag in ${path}`);
  styles.set(path, css);
}

async function moduleSource(path) {
  const name = relative(directory, path).split(sep).join("/");
  if (modules[name]) return name;
  const source = await readFile(path, "utf8");
  const imports = {};
  modules[name] = { source, imports };
  for (const match of source.matchAll(/\b(?:import|export)\s+(?:[^"'();]*?\s+from\s*)?["']([^"']+)["']/g)) {
    if (!match[1].startsWith(".")) throw new Error(`Non-local module dependency in ${name}: ${match[1]}`);
    imports[match[1]] = await moduleSource(localPath(match[1], dirname(path)));
  }
  if (/\bimport\s*\(/.test(source)) throw new Error(`Dynamic imports need an explicit bundle entry: ${name}`);
  return name;
}

function attribute(tag, name) {
  return tag.match(new RegExp(`\\b${name}\\s*=\\s*(["'])(.*?)\\1`, "i"))?.[2];
}

async function page(name) {
  let html = await readFile(localPath(name), "utf8");
  const entries = [];
  for (const match of [...html.matchAll(/<script\b[^>]*\bsrc\s*=\s*["'][^"']+["'][^>]*>\s*<\/script\s*>/gi)]) {
    if (attribute(match[0], "type") !== "module") throw new Error(`Expected module script in ${name}`);
    entries.push(await moduleSource(localPath(attribute(match[0], "src"))));
    html = html.replace(match[0], "");
  }
  if (entries.length !== 1) throw new Error(`Expected one entry module in ${name}`);
  for (const match of [...html.matchAll(/<link\b[^>]*>/gi)]) {
    if (attribute(match[0], "rel") !== "stylesheet") continue;
    await stylesheet(localPath(attribute(match[0], "href")));
    html = html.replace(match[0], "");
  }
  for (const match of [...html.matchAll(/<(?:img|image|source)\b[^>]*>/gi)]) {
    const attr = attribute(match[0], "src") === undefined ? "href" : "src";
    const reference = attribute(match[0], attr);
    if (!reference || reference.startsWith("data:") || reference.startsWith("#")) continue;
    html = html.replace(match[0], match[0].replace(reference, await dataURL(localPath(reference))));
  }
  html = html.replace(/href=(["'])\.\/(?:index\.html)?\1/g, 'href="https://lazarus-coder.github.io/yi-go/"')
    .replace(/href=(["'])\.\/play\.html\1/g, 'href="#play"')
    .replace(/href=(["'])\.\/watch\.html\1/g, 'href="#watch"')
    .replace(/href=(["'])\.\/join\.html\1/g, 'href="https://lazarus-coder.github.io/yi-go/join.html"');
  for (const match of html.matchAll(/<a\b[^>]*>/gi)) {
    const href = attribute(match[0], "href");
    if (href && !/^(?:#|https?:|mailto:)/i.test(href)) throw new Error(`Unresolved offline navigation in ${name}: ${href}`);
  }
  const body = html.match(/<body\b[^>]*>([\s\S]*?)<\/body\s*>/i)?.[1];
  if (body === undefined) throw new Error(`Missing body in ${name}`);
  return { html, body, entry: entries[0], title: html.match(/<title>([\s\S]*?)<\/title>/i)?.[1] || "弈" };
}

const play = await page("play.html");
const watch = await page("watch.html");
let worker = await readFile(localPath("engine-worker.js"), "utf8");
for (const asset of ["wasm_exec.js", "game.wasm"]) {
  const quoted = new RegExp(`(["'])\\.\\/${asset.replaceAll(".", "\\.")}\\1`, "g");
  if (!quoted.test(worker)) throw new Error(`Worker asset reference not found: ${asset}`);
  quoted.lastIndex = 0;
  worker = worker.replace(quoted, JSON.stringify(await dataURL(localPath(asset))));
}

const assets = JSON.stringify({ modules, worker, play: { entry: play.entry }, watch: { body: watch.body, entry: watch.entry, title: watch.title } }).replaceAll("<", "\\u003c");
const bootstrap = `
const assets = JSON.parse(document.getElementById("yi-offline-assets").textContent);
const mode = () => location.hash === "#watch" ? "watch" : "play";
const activeMode = mode();
if (activeMode === "watch") {
  document.title = assets.watch.title;
  document.body.innerHTML = assets.watch.body;
}
addEventListener("hashchange", () => { if (mode() !== activeMode) location.reload(); });
const workerURL = URL.createObjectURL(new Blob([assets.worker], { type: "text/javascript" }));
const urls = new Map();
function moduleURL(name) {
  if (urls.has(name)) return urls.get(name);
  const module = assets.modules[name];
  let source = module.source;
  for (const [specifier, dependency] of Object.entries(module.imports)) {
    const replacement = JSON.stringify(moduleURL(dependency));
    source = source.split('"' + specifier + '"').join(replacement).split("'" + specifier + "'").join(replacement);
  }
  source = source.replace(/new URL\\((["'])\\.\\/engine-worker\\.js\\1,\\s*import\\.meta\\.url\\)/g, JSON.stringify(workerURL));
  const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
  urls.set(name, url);
  return url;
}
try {
  await import(moduleURL(assets[activeMode].entry));
} catch (error) {
  console.error(error);
  const target = document.getElementById("error") || document.getElementById("watch-status");
  if (target) { target.hidden = false; target.textContent = "离线页面未能启动，请使用较新的浏览器重新打开。"; }
}
`;
const html = play.html.replace(/data-engine=(["'])http\1/, 'data-engine="wasm"')
  .replace(/<html\b/, '<html data-standalone="true"')
  .replace("</head>", `<style>\n${[...styles.values()].join("\n")}\n</style>\n</head>`)
  .replace("</body>", `<script id="yi-offline-assets" type="application/json">${assets}</script>\n<script type="module">${bootstrap}</script>\n</body>`);
const output = localPath("yi-go.html");
await writeFile(output, html);
console.log(`Self-contained play + watch HTML: ${output} (${(Buffer.byteLength(html) / 1024 / 1024).toFixed(2)} MiB)`);
