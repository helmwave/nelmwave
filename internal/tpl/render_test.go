package tpl

import (
	"context"
	"strings"
	"testing"
	"text/template"
)

func TestRender_DefaultDelimitersAndEnv(t *testing.T) {
	t.Setenv("NW_TEST_ENV", "prod")
	out, err := Render(context.Background(), "t",
		[]byte(`env: [[ getenv "NW_TEST_ENV" ]] up: [[ strings.ToUpper "hi" ]]`),
		Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := string(out); got != "env: prod up: HI" {
		t.Errorf("unexpected render output: %q", got)
	}
}

func TestRender_GetenvDefault(t *testing.T) {
	out, err := Render(context.Background(), "t",
		[]byte(`[[ getenv "NW_DOES_NOT_EXIST_123" "fallback" ]]`), Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(out) != "fallback" {
		t.Errorf("want fallback, got %q", out)
	}
}

func TestRender_CustomFuncs(t *testing.T) {
	out, err := Render(context.Background(), "t", []byte(`[[ shout ]]`), Options{
		Funcs: template.FuncMap{"shout": func() string { return "HEY" }},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(out) != "HEY" {
		t.Errorf("want HEY, got %q", out)
	}
}

func TestRender_ErrorSurfacesTemplateName(t *testing.T) {
	// An empty action ([[ ]]) is a parse error; the message should name the template.
	_, err := Render(context.Background(), "broken.tpl", []byte(`x: [[ ]]`), Options{})
	if err == nil {
		t.Fatal("expected parse error for empty action")
	}
	if !strings.Contains(err.Error(), "broken.tpl") {
		t.Errorf("error should mention template name, got: %v", err)
	}
}
