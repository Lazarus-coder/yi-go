package match

import (
	"errors"

	"yi-go/internal/game"
)

// AnalysisRequest is used only by the WebAssembly bridge, not the HTTP API.
type AnalysisRequest struct {
	Request    Request `json:"request"`
	Shard      int     `json:"shard"`
	Shards     int     `json:"shards"`
	Depth      int     `json:"depth"`
	NodeBudget int     `json:"nodeBudget"`
	ThinkMS    int     `json:"thinkMS"`
}

type AnalysisResult struct {
	Game     *Result            `json:"game,omitempty"`
	Analysis *game.SearchResult `json:"analysis,omitempty"`
	Config   *game.SearchConfig `json:"config,omitempty"`
}

type MergeRequest struct {
	Request  Request             `json:"request"`
	Analyses []game.SearchResult `json:"analyses"`
}

// Analyze evaluates one disjoint root-move shard. NodeBudget is per root,
// keeping evaluations identical when the browser changes its worker count.
func Analyze(input AnalysisRequest) (AnalysisResult, error) {
	position, err := prepare(input.Request)
	if err != nil {
		return AnalysisResult{}, err
	}
	if position.Over || position.Next != game.White {
		return AnalysisResult{Game: position}, nil
	}
	config := position.SearchConfig()
	if config.Roots == 0 {
		finished, err := applyReply(position, position.ChooseMove(), nil)
		return AnalysisResult{Game: finished}, err
	}
	if input.Shards == 0 {
		input.Shards = 1
	}
	if input.Depth == 0 {
		input.Depth = 1
	}
	if input.NodeBudget == 0 {
		input.NodeBudget = config.NodeBudget
	}
	if input.ThinkMS == 0 {
		input.ThinkMS = config.ThinkMS
	}
	if input.Shards < 1 || input.Shards > config.Roots || input.Shard < 0 || input.Shard >= input.Shards ||
		input.Depth < 1 || input.Depth > config.MaxDepth || input.NodeBudget < 1 || input.NodeBudget > config.NodeBudget ||
		input.ThinkMS < 1 || input.ThinkMS > config.ThinkMS {
		return AnalysisResult{}, errors.New("搜索分片参数无效")
	}
	analysis := position.SearchShard(input.Shard, input.Shards, input.Depth, input.NodeBudget, input.ThinkMS)
	return AnalysisResult{Analysis: &analysis, Config: &config}, nil
}

// Merge accepts only a complete search round, then revalidates the selected
// move against the original request instead of trusting any worker's state.
func Merge(input MergeRequest) (*Result, error) {
	position, err := prepare(input.Request)
	if err != nil {
		return nil, err
	}
	if position.Over || position.Next != game.White || position.SearchConfig().Roots == 0 {
		return nil, errors.New("当前棋局无需合并搜索结果")
	}
	move, err := position.MergeSearch(input.Analyses)
	if err != nil {
		return nil, err
	}
	stats := &SearchStats{Depth: input.Analyses[0].Depth}
	for _, analysis := range input.Analyses {
		if analysis.Roots > 0 {
			stats.Workers++
		}
	}
	return applyReply(position, move, stats)
}
