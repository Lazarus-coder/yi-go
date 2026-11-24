import assert from "node:assert/strict";
import test from "node:test";
import { setImmediate } from "node:timers/promises";

test("a restored final board announces intersections without accepting moves", async (t) => {
  class Element extends EventTarget {
    attributes = new Map();
    setAttribute(name, value) { this.attributes.set(name, String(value)); }
    toggleAttribute(name, enabled) {
      if (enabled) this.attributes.set(name, "");
      else this.attributes.delete(name);
    }
    append() {}
    replaceChildren() {}
  }
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  }
  const moves = [{ x: 5, y: 4 }, { x: 6, y: 4 }];
  const saved = { size: 9, style: "china", moves, resigned: 1 };
  const stones = Array(81).fill(0);
  stones[4 * 9 + 5] = 1;
  stones[4 * 9 + 6] = 2;
  const game = { ...saved, board: stones, captures: { black: 0, white: 0 }, over: true, next: 1 };
  globalThis.document = {
    documentElement: { dataset: { engine: "http" } },
    getElementById: element,
    createElementNS: () => new Element(),
  };
  const originalStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: {
    getItem: () => JSON.stringify(saved), setItem() {},
  } });
  t.after(() => {
    delete globalThis.document;
    if (originalStorage) Object.defineProperty(globalThis, "localStorage", originalStorage);
    else delete globalThis.localStorage;
  });
  const fetch = t.mock.method(globalThis, "fetch", async () => Response.json(game));
  await import("../web/app.js");
  await setImmediate();

  const board = element("board");
  function press(key) {
    board.dispatchEvent(Object.assign(new Event("keydown", { cancelable: true }), { key }));
  }
  assert.equal(board.attributes.get("aria-disabled"), "false");
  for (const expected of ["F5，黑棋", "G5，白棋", "H5，空点"]) {
    press("ArrowRight");
    assert.equal(element("board-announcement").textContent, expected);
    assert.equal(element("cursor").attributes.has("hidden"), false);
  }
  press("Enter");
  press(" ");
  assert.equal(fetch.mock.callCount(), 1, "only the initial saved-game sync is sent");
});
