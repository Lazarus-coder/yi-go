package game

import (
	"errors"
	"math"
	"sync"
	"time"
)

// SearchConfig is also sent to browsers so worker pools need no persona IDs.
// NodeBudget applies separately to each root, independent of the worker count.
type SearchConfig struct {
	Roots      int `json:"roots"`
	MaxDepth   int `json:"maxDepth"`
	NodeBudget int `json:"nodeBudget"`
	ThinkMS    int `json:"thinkMS"`
	width      int
}

var specialistSearch = SearchConfig{Roots: 32, MaxDepth: 6, NodeBudget: 8192, ThinkMS: 150, width: 5}
var intensiveSearch = SearchConfig{Roots: 64, MaxDepth: 8, NodeBudget: 8192, ThinkMS: 300, width: 6}

type SearchStats struct {
	Workers   int   `json:"workers"`
	Depth     int   `json:"depth"`
	Nodes     int   `json:"nodes"`
	Roots     int   `json:"roots"`
	ElapsedMS int64 `json:"elapsedMS,omitempty"`
}

type SearchResult struct {
	Move       Move    `json:"move"`
	Score      float64 `json:"score"`
	Rank       int     `json:"rank"`
	Depth      int     `json:"depth"`
	Nodes      int     `json:"nodes"`
	Roots      int     `json:"roots"`
	TotalRoots int     `json:"totalRoots"`
	Shard      int     `json:"shard"`
	Shards     int     `json:"shards"`
	Complete   bool    `json:"complete"`
}

func (g *Game) SearchConfig() SearchConfig {
	return profileFor(g.Style).search
}

// BetterThan compares scores only at the same depth; MergeSearch checks the
// complete round first. Rank resolves ties independently of completion order.
func (r SearchResult) BetterThan(other SearchResult) bool {
	return r.Roots > 0 && (other.Roots == 0 || r.Score > other.Score ||
		(r.Score == other.Score && r.Rank < other.Rank))
}

// MergeSearch rejects partial, mixed-depth or duplicate shards. Comparing a
// shallow root against a deeper one would reward whichever worker finished first.
func (g *Game) MergeSearch(results []SearchResult) (Move, error) {
	config := g.SearchConfig()
	invalid := errors.New("搜索结果不完整，请重试")
	if len(results) == 0 || len(results) > config.Roots {
		return Move{}, invalid
	}
	first := results[0]
	if first.Shards != len(results) || first.Depth < 1 || first.Depth > config.MaxDepth ||
		first.TotalRoots < 1 || first.TotalRoots > config.Roots {
		return Move{}, invalid
	}
	seen := make([]bool, len(results))
	best := SearchResult{}
	for _, result := range results {
		if !result.Complete || result.Depth != first.Depth || result.TotalRoots != first.TotalRoots ||
			result.Shards != len(results) || result.Shard < 0 || result.Shard >= len(results) || seen[result.Shard] ||
			math.IsNaN(result.Score) || math.IsInf(result.Score, 0) || result.Nodes < 0 {
			return Move{}, invalid
		}
		seen[result.Shard] = true
		assigned := max(0, (result.TotalRoots+result.Shards-1-result.Shard)/result.Shards)
		if result.Roots != assigned || (result.Roots > 0 &&
			(result.Rank < 0 || result.Rank >= result.TotalRoots || result.Rank%result.Shards != result.Shard)) {
			return Move{}, invalid
		}
		if result.BetterThan(best) {
			best = result
		}
	}
	return best.Move, nil
}

type searchPlan struct {
	seeds, roots []candidate
	width        int
}

func (g *Game) planSearch(ranked []candidate, config SearchConfig) searchPlan {
	count := config.Roots
	if g.Passes > 0 {
		count-- // Reserve room for ending the game by passing.
	}
	seeds := ranked[:min(len(ranked), max(0, count))]
	roots := append([]candidate(nil), seeds...)
	if g.Passes > 0 || len(roots) == 0 {
		roots = append(roots, candidate{move: Move{-1, -1}})
	}
	return searchPlan{seeds: seeds, roots: roots, width: config.width}
}

// SearchShard is the deterministic kernel shared by native goroutines and WASM
// workers. Root r belongs only to shard r%shards, with no shared mutable state.
// shortcut: selective replies can miss distant tesuji; use a full Go engine for competitive play.
func (g *Game) SearchShard(shard, shards, depth, nodeBudget int, thinkMS ...int) SearchResult {
	start := time.Now()
	config := g.SearchConfig()
	if config.Roots == 0 || shard < 0 || shards < 1 || shards > config.Roots || shard >= shards ||
		depth < 1 || depth > config.MaxDepth || nodeBudget < 1 || nodeBudget > config.NodeBudget || g.Over ||
		len(thinkMS) > 1 || (len(thinkMS) == 1 && (thinkMS[0] < 1 || thinkMS[0] > config.ThinkMS)) {
		return SearchResult{Move: Move{-1, -1}, Shard: shard, Shards: shards, Depth: depth}
	}
	deadline := time.Time{}
	if len(thinkMS) == 1 {
		deadline = start.Add(time.Duration(thinkMS[0]) * time.Millisecond)
	}
	plan := g.planSearch(g.rankMoves(g.Style), config)
	return g.searchShard(plan, shard, shards, depth, nodeBudget, deadline)
}

func (g *Game) searchShard(plan searchPlan, shard, shards, depth, nodeBudget int, deadline time.Time) SearchResult {
	result := SearchResult{Move: Move{-1, -1}, Rank: len(plan.roots), Depth: depth,
		Shard: shard, Shards: shards, TotalRoots: len(plan.roots), Complete: true,
		Roots: max(0, (len(plan.roots)+shards-1-shard)/shards)}
	for rank := shard; rank < len(plan.roots); rank += shards {
		option := plan.roots[rank]
		search := treeSearch{limit: nodeBudget, width: plan.width, seeds: plan.seeds, deadline: deadline}
		child, legal := search.child(g, option.move)
		if !legal {
			result.Nodes += search.nodes
			result.Complete = false
			return result
		}
		value, complete := search.negamax(child, depth-1, math.Inf(-1), math.Inf(1))
		result.Nodes += search.nodes
		if !complete {
			result.Complete = false
			return result
		}
		value = -value
		if g.Style == "alphago" {
			// Trust the root policy less as concrete replies become visible.
			// Twelve evaluation units are worth one healthy stone.
			if option.move.X >= 0 {
				value += (4 / float64(depth)) * max(-6, option.score-plan.roots[0].score)
			}
		} else {
			value += min(100, max(-100, option.score)) * 0.005
		}
		if result.Rank == len(plan.roots) || value > result.Score {
			result.Move, result.Score, result.Rank = option.move, value, rank
		}
	}
	return result
}

func (g *Game) searchNative(ranked []candidate, config SearchConfig, workers int, start time.Time) (Move, SearchStats) {
	plan := g.planSearch(ranked, config)
	workers = max(1, min(workers, len(plan.roots)))
	stats := SearchStats{Roots: len(plan.roots)}
	best := plan.roots[0].move
	deadline := start.Add(time.Duration(config.ThinkMS) * time.Millisecond)
	lastDuration := time.Duration(0)
	for depth := 1; depth <= config.MaxDepth; depth++ {
		// Learn the cost from this position on this machine, without a delay or
		// a global calibration that becomes stale when CPU load changes.
		if !time.Now().Before(deadline) || (depth > 2 && time.Until(deadline) < lastDuration*3) {
			break
		}
		iterationStart := time.Now()
		results := make([]SearchResult, workers)
		var wait sync.WaitGroup
		for shard := range results {
			wait.Add(1)
			go func(shard int) {
				defer wait.Done()
				results[shard] = g.searchShard(plan, shard, workers, depth, config.NodeBudget, deadline)
			}(shard)
		}
		wait.Wait()
		for _, result := range results {
			stats.Nodes += result.Nodes
		}
		move, err := g.MergeSearch(results)
		if err != nil {
			break
		}
		best, stats.Depth, stats.Workers = move, depth, workers
		lastDuration = time.Since(iterationStart)
	}
	stats.ElapsedMS = time.Since(start).Milliseconds()
	return best, stats
}
