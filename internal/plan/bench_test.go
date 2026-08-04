package plan

import (
	"fmt"
	"testing"

	"github.com/helmwave/nelmwave/internal/config"
)

// Two layers: half the releases are databases, the other half are apps that
// select every database by label. That is the shape resolution is worst at —
// each app is compared against every release — while staying acyclic.
func benchCfg(n int) *config.Config {
	releases := make(map[string]config.Release, n)
	for i := range n {
		if i%2 == 0 {
			releases[fmt.Sprintf("db%d@ns", i)] = config.Release{
				Chart:  config.Chart{Name: "repo/c"},
				Labels: map[string]string{"tier": "db", "idx": fmt.Sprint(i)},
			}
			continue
		}
		releases[fmt.Sprintf("app%d@ns", i)] = config.Release{
			Chart:  config.Chart{Name: "repo/c"},
			Labels: map[string]string{"tier": "app", "idx": fmt.Sprint(i)},
			Needs:  config.Needs{MatchLabels: map[string]string{"tier": "db"}},
		}
	}
	return &config.Config{Releases: releases}
}

func BenchmarkFromConfig_200Releases(b *testing.B) {
	cfg := benchCfg(200)
	b.ReportAllocs()
	for b.Loop() {
		FromConfig(cfg)
	}
}

func BenchmarkValidate_200Releases(b *testing.B) {
	cfg := benchCfg(200)
	b.ReportAllocs()
	for b.Loop() {
		if err := config.Validate(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateAndPlan mirrors what `build` actually does: validate a fresh
// config, then project it. Both walk the needs graph, so this is where the
// resolution cache pays off — the fixture is rebuilt each iteration so the cache
// starts cold, exactly as it does per invocation.
func BenchmarkValidateAndPlan_200Releases(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		cfg := benchCfg(200)
		b.StartTimer()

		if err := config.Validate(cfg); err != nil {
			b.Fatal(err)
		}
		FromConfig(cfg)
	}
}
