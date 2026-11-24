//go:build js && wasm

// The browser uses the same rules and CPU as the HTTP server.
package main

import (
	"encoding/json"
	"io"
	"strings"
	"syscall/js"

	"github.com/Lazarus-coder/yi-go/internal/match"
)

type response struct {
	match.AnalysisResult
	Error string `json:"error,omitempty"`
}

// All three entry points retain the same strict, bounded JSON boundary.
func bind[T any](name string, handle func(T) (match.AnalysisResult, error)) {
	js.Global().Set(name, js.FuncOf(func(_ js.Value, args []js.Value) any {
		result := response{}
		if len(args) != 1 || args[0].Type() != js.TypeString || len(args[0].String()) > 64<<10 {
			result.Error = "请求格式不正确。"
		} else {
			var input T
			decoder := json.NewDecoder(strings.NewReader(args[0].String()))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				result.Error = "请求格式不正确。"
			} else if err := decoder.Decode(&struct{}{}); err != io.EOF {
				result.Error = "请求只能包含一局棋。"
			} else {
				value, err := handle(input)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.AnalysisResult = value
				}
			}
		}
		data, err := json.Marshal(result)
		if err != nil {
			return `{"error":"棋局暂时无法读取，请刷新后重试。"}`
		}
		return string(data)
	}))
}

func main() {
	bind("yiRun", func(input match.Request) (match.AnalysisResult, error) {
		g, err := match.Run(input)
		return match.AnalysisResult{Game: g}, err
	})
	bind("yiAnalyze", match.Analyze)
	bind("yiMerge", func(input match.MergeRequest) (match.AnalysisResult, error) {
		g, err := match.Merge(input)
		return match.AnalysisResult{Game: g}, err
	})
	js.Global().Call("yiReady")
	select {}
}
