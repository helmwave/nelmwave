// Package graph runs a function over a dependency DAG with bounded concurrency.
//
// Independent branches run in parallel; a node runs only after all of its
// dependencies have succeeded. If a dependency fails (or is skipped), the node
// is skipped too, but independent branches keep going — fail-fast per branch,
// not globally. The caller inspects the returned per-node results.
package graph

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Result is the outcome for a single node.
type Result struct {
	// Err is nil on success. On failure it is the node function's error; on a
	// skip it explains which dependency caused it. Skipped is true only for the
	// latter.
	Err     error
	Skipped bool
}

// Run executes fn for every node, where deps[node] lists the nodes that must
// succeed before it. concurrency bounds how many fn calls run at once (<=0 means
// no limit). The dependency graph must be acyclic (validated upstream); a cycle
// would deadlock. Run returns a result per node and never returns early.
func Run(ctx context.Context, deps map[string][]string, concurrency int, fn func(context.Context, string) error) map[string]Result {
	nodes := nodeSet(deps)
	if concurrency <= 0 || concurrency > len(nodes) {
		concurrency = max(len(nodes), 1)
	}

	done := make(map[string]chan struct{}, len(nodes))
	for n := range nodes {
		done[n] = make(chan struct{})
	}

	var mu sync.Mutex
	results := make(map[string]Result, len(nodes))
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for n := range nodes {
		wg.Add(1)
		go func(node string) {
			defer wg.Done()
			defer close(done[node])

			// Wait for every dependency to finish, and skip if any failed.
			for _, dep := range deps[node] {
				ch, ok := done[dep]
				if !ok {
					continue // dependency outside the node set — ignore
				}
				<-ch
				mu.Lock()
				depFailed := results[dep].Err != nil
				mu.Unlock()
				if depFailed {
					mu.Lock()
					results[node] = Result{Err: fmt.Errorf("skipped: dependency %q did not succeed", dep), Skipped: true}
					mu.Unlock()
					return
				}
			}

			if err := ctx.Err(); err != nil {
				mu.Lock()
				results[node] = Result{Err: err, Skipped: true}
				mu.Unlock()
				return
			}

			sem <- struct{}{}
			err := fn(ctx, node)
			<-sem

			mu.Lock()
			results[node] = Result{Err: err}
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	return results
}

// Reverse flips edge direction: if a depends on b in deps, then in the result b
// depends on a. Used to uninstall in reverse dependency order.
func Reverse(deps map[string][]string) map[string][]string {
	out := make(map[string][]string, len(deps))
	for n := range deps {
		if _, ok := out[n]; !ok {
			out[n] = nil
		}
	}
	for node, ds := range deps {
		for _, d := range ds {
			out[d] = append(out[d], node)
		}
	}
	for n := range out {
		sort.Strings(out[n])
	}
	return out
}

// nodeSet collects every node mentioned in deps (as a key or a dependency).
func nodeSet(deps map[string][]string) map[string]struct{} {
	nodes := make(map[string]struct{}, len(deps))
	for n, ds := range deps {
		nodes[n] = struct{}{}
		for _, d := range ds {
			nodes[d] = struct{}{}
		}
	}
	return nodes
}
