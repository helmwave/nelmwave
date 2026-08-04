package datasource

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeValues_DeepMergeAndOverride(t *testing.T) {
	global := []byte(`
resources:
  requests:
    cpu: 50m
    memory: 64Mi
tags: [a, b]
replicas: 1
`)
	release := []byte(`
resources:
  requests:
    cpu: 250m
tags: [c]
replicas: 3
`)
	out, err := MergeValues([][]byte{global, release})
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}

	req := got["resources"].(map[string]any)["requests"].(map[string]any)
	if req["cpu"] != "250m" {
		t.Errorf("cpu should be overridden to 250m, got %v", req["cpu"])
	}
	if req["memory"] != "64Mi" {
		t.Errorf("memory should survive the deep merge, got %v", req["memory"])
	}
	if replicas := got["replicas"]; replicas != 3 {
		t.Errorf("scalar should be replaced, got %v", replicas)
	}
	if tags, ok := got["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "c" {
		t.Errorf("sequence should be replaced wholesale, got %v", got["tags"])
	}
}

func TestMergeValues_SkipsEmptyDocs(t *testing.T) {
	out, err := MergeValues([][]byte{[]byte("   \n"), []byte("a: 1\n"), nil})
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 {
		t.Errorf("want a=1, got %v", got)
	}
}

func TestMergeValues_RejectsNonMapDoc(t *testing.T) {
	if _, err := MergeValues([][]byte{[]byte("- just\n- a\n- list\n")}); err == nil {
		t.Error("expected error for non-map values document")
	}
}
