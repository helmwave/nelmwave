package graph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_RespectsDependencyOrder(t *testing.T) {
	// c depends on b depends on a.
	deps := map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}}

	var mu sync.Mutex
	var order []string
	res := Run(context.Background(), deps, 4, func(_ context.Context, n string) error {
		mu.Lock()
		order = append(order, n)
		mu.Unlock()
		return nil
	})

	for n, r := range res {
		if r.Err != nil {
			t.Errorf("node %q: unexpected err %v", n, r.Err)
		}
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("order = %v, want [a b c]", order)
	}
}

func TestRun_ParallelIndependentBranches(t *testing.T) {
	// Two independent nodes must be able to run at the same time: each signals
	// arrival, then waits for the other. If they were serialized, the wait would
	// time out.
	deps := map[string][]string{"a": nil, "b": nil}
	var wg sync.WaitGroup
	wg.Add(2)
	bothStarted := make(chan struct{})
	go func() { wg.Wait(); close(bothStarted) }()

	Run(context.Background(), deps, 2, func(_ context.Context, _ string) error {
		wg.Done()
		select {
		case <-bothStarted:
		case <-time.After(2 * time.Second):
		}
		return nil
	})

	select {
	case <-bothStarted:
	default:
		t.Error("independent nodes did not run concurrently")
	}
}

func TestRun_FailureSkipsDependentsButNotSiblings(t *testing.T) {
	// b needs a (a fails -> b skipped); c is independent (runs).
	deps := map[string][]string{"a": nil, "b": {"a"}, "c": nil}
	boom := errors.New("boom")
	var cRan atomic.Bool
	res := Run(context.Background(), deps, 4, func(_ context.Context, n string) error {
		if n == "a" {
			return boom
		}
		if n == "c" {
			cRan.Store(true)
		}
		return nil
	})

	if !errors.Is(res["a"].Err, boom) {
		t.Errorf("a should carry its error, got %v", res["a"].Err)
	}
	if !res["b"].Skipped {
		t.Errorf("b should be skipped after a failed, got %+v", res["b"])
	}
	if res["c"].Err != nil || !cRan.Load() {
		t.Errorf("independent c should have run, got %+v ran=%v", res["c"], cRan.Load())
	}
}

func TestReverse(t *testing.T) {
	deps := map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}}
	rev := Reverse(deps)
	// a is now depended-on-by b: in reverse, a depends on b.
	if len(rev["a"]) != 1 || rev["a"][0] != "b" {
		t.Errorf("rev[a] = %v, want [b]", rev["a"])
	}
	if len(rev["c"]) != 0 {
		t.Errorf("rev[c] = %v, want []", rev["c"])
	}
}
