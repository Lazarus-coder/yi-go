//go:build js && wasm

// The browser uses the same rules and CPU as the HTTP server.
package main

import (
	"encoding/json"
	"io"
	"strings"
	"syscall/js"

	"yi-go/internal/game"
	"yi-go/internal/match"
)

type response struct {
	Game  *game.Game `json:"game,omitempty"`
	Error string     `json:"error,omitempty"`
}

func run(_ js.Value, args []js.Value) any {
	result := response{}
	if len(args) != 1 || args[0].Type() != js.TypeString || len(args[0].String()) > 64<<10 {
		result.Error = "请求格式不正确。"
	} else {
		var input match.Request
		decoder := json.NewDecoder(strings.NewReader(args[0].String()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			result.Error = "请求格式不正确。"
		} else if err := decoder.Decode(&struct{}{}); err != io.EOF {
			result.Error = "请求只能包含一局棋。"
		} else {
			g, err := match.Run(input)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Game = g
			}
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return `{"error":"棋局暂时无法读取，请刷新后重试。"}`
	}
	return string(data)
}

func main() {
	js.Global().Set("yiRun", js.FuncOf(run))
	js.Global().Call("yiReady")
	select {}
}
