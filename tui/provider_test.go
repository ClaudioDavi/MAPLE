package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	cases := []struct {
		in        string
		wantName  string
		wantError bool
	}{
		{"anthropic", "anthropic", false},
		{"claude", "anthropic", false},
		{"openai", "openai", false},
		{"copilot", "github-copilot", false},
		{"github-copilot", "github-copilot", false},
		{"bedrock", "amazon-bedrock", false},
		{"AWS", "amazon-bedrock", false},
		{"  Bedrock  ", "amazon-bedrock", false},
		{"", "", false}, // empty = no error, no preset
		{"nope", "", true},
	}
	for _, c := range cases {
		got, err := resolveProvider(c.in)
		if c.wantError {
			if err == nil {
				t.Errorf("resolveProvider(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveProvider(%q): unexpected error %v", c.in, err)
			continue
		}
		if c.wantName == "" {
			if got != nil {
				t.Errorf("resolveProvider(%q): expected nil, got %s", c.in, got.Name)
			}
			continue
		}
		if got == nil || got.Name != c.wantName {
			t.Errorf("resolveProvider(%q): want %s, got %v", c.in, c.wantName, got)
		}
	}
}

func TestInjectModelFrontmatter_Inserts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmode: subagent\n---\n\nBody text.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := injectModelFrontmatter(path, "amazon-bedrock/foo", "amazon-bedrock", true)
	if err != nil || !changed {
		t.Fatalf("inject: changed=%v err=%v", changed, err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "\nmodel: amazon-bedrock/foo\n---") {
		t.Errorf("model line not inserted before closing ---:\n%s", out)
	}
}

func TestInjectModelFrontmatter_PreservesUserCustom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmodel: my-corp/private-v1\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := injectModelFrontmatter(path, "openai/gpt-5", "openai", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no change for user-customized model: line")
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "model: my-corp/private-v1") {
		t.Errorf("user model overwritten: %s", out)
	}
}

func TestInjectModelFrontmatter_OverwritesManaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmodel: openai/gpt-5-mini\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := injectModelFrontmatter(path, "amazon-bedrock/sonnet", "amazon-bedrock", true)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "model: amazon-bedrock/sonnet") {
		t.Errorf("managed model not overwritten: %s", out)
	}
	if strings.Contains(string(out), "openai/gpt-5-mini") {
		t.Errorf("old managed model still present: %s", out)
	}
}

func TestInjectModelFrontmatter_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmodel: openai/gpt-5\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, _ := injectModelFrontmatter(path, "openai/gpt-5", "openai", true)
	if changed {
		t.Error("expected no change when value already matches")
	}
}

func TestInjectModelFrontmatter_ReplayPreservesOtherProvider(t *testing.T) {
	// On a non-explicit replay (maple update), an agent the user hand-pointed at
	// a different managed provider must survive.
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmodel: openai/gpt-5\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := injectModelFrontmatter(path, "amazon-bedrock/sonnet", "amazon-bedrock", false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("replay should not overwrite a different provider's model line")
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "model: openai/gpt-5") {
		t.Errorf("cross-provider hand-edit lost on replay: %s", out)
	}
}

func TestInjectModelFrontmatter_ExplicitSwitchOverwritesOtherProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	body := "---\nname: dotnet\nmodel: openai/gpt-5\n---\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := injectModelFrontmatter(path, "amazon-bedrock/sonnet", "amazon-bedrock", true)
	if err != nil || !changed {
		t.Fatalf("explicit switch should overwrite: changed=%v err=%v", changed, err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "model: amazon-bedrock/sonnet") {
		t.Errorf("explicit switch did not apply: %s", out)
	}
}

func TestWriteClaudeEnv_OverrideFlowsThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provider.env")
	p, _ := resolveProvider("bedrock")
	override := "amazon-bedrock/us.anthropic.claude-opus-4-8"
	models := map[providerTier]string{tierDeep: override, tierFast: override, tierSmall: override}
	if err := writeClaudeEnv(path, p, models); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	bare := "us.anthropic.claude-opus-4-8"
	if !strings.Contains(string(data), "ANTHROPIC_MODEL="+bare+"\n") {
		t.Errorf("override not in ANTHROPIC_MODEL:\n%s", data)
	}
	if !strings.Contains(string(data), "ANTHROPIC_SMALL_FAST_MODEL="+bare+"\n") {
		t.Errorf("override not in ANTHROPIC_SMALL_FAST_MODEL:\n%s", data)
	}
}

func TestReqProviderRuntime(t *testing.T) {
	cwd := t.TempDir()
	_ = os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte("{}"), 0o644)
	p, _ := resolveProvider("bedrock")
	applyProvider(p, "", cwd, true)

	// AWS_REGION already exported → static value must not be re-added.
	preset, env, model := reqProviderRuntime(cwd, []string{"AWS_REGION=eu-west-1"})
	if preset == nil || preset.Name != "amazon-bedrock" {
		t.Fatalf("preset: %v", preset)
	}
	if !strings.HasPrefix(model, "amazon-bedrock/us.anthropic.claude-opus") {
		t.Errorf("deep-tier model not returned: %q", model)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "AWS_REGION=") {
		t.Errorf("already-set AWS_REGION should not be re-added: %s", joined)
	}
	if !strings.Contains(joined, "CLAUDE_CODE_USE_BEDROCK=1") {
		t.Errorf("missing CLAUDE_CODE_USE_BEDROCK: %s", joined)
	}
	if !strings.Contains(joined, "ANTHROPIC_MODEL=us.anthropic.claude-sonnet") {
		t.Errorf("missing derived ANTHROPIC_MODEL: %s", joined)
	}
}

func TestReqProviderRuntime_NoState(t *testing.T) {
	cwd := t.TempDir()
	preset, env, model := reqProviderRuntime(cwd, os.Environ())
	if preset != nil || env != nil || model != "" {
		t.Errorf("expected empty runtime with no state, got %v / %v / %q", preset, env, model)
	}
}

func TestPatchOpencodeJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	initial := `{"theme":"catppuccin","default_agent":"orchestrator","provider":{"existing":{"keep":"me"}}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := resolveProvider("bedrock")
	if err := patchOpencodeJSON(path, p, "amazon-bedrock/foo"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["theme"] != "catppuccin" {
		t.Error("preserved field theme was lost")
	}
	if doc["model"] != "amazon-bedrock/foo" {
		t.Error("default model not set")
	}
	prov, _ := doc["provider"].(map[string]any)
	if prov["amazon-bedrock"] == nil {
		t.Error("amazon-bedrock block not added")
	}
	if prov["existing"] == nil {
		t.Error("existing provider block was clobbered")
	}
}

func TestApplyProvider_PersistsState(t *testing.T) {
	cwd := t.TempDir()
	// Minimum scaffolding
	_ = os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte("{}"), 0o644)

	p, _ := resolveProvider("openai")
	applyProvider(p, "openai/gpt-5", cwd, true)

	prov, model := loadProviderState(cwd)
	if prov != "openai" {
		t.Errorf("provider state: got %q", prov)
	}
	if model != "openai/gpt-5" {
		t.Errorf("model_override state: got %q", model)
	}
}

func TestApplyProvider_WritesClaudeEnvForBedrock(t *testing.T) {
	cwd := t.TempDir()
	_ = os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte("{}"), 0o644)
	p, _ := resolveProvider("bedrock")
	applyProvider(p, "", cwd, true)

	envPath := filepath.Join(cwd, "scripts", "maple", "provider.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("provider.env not written: %v", err)
	}
	want := []string{
		"CLAUDE_CODE_USE_BEDROCK=1",
		"AWS_REGION=${AWS_REGION:-us-east-1}",
		"ANTHROPIC_MODEL=us.anthropic.claude-sonnet-4-6",
		"ANTHROPIC_SMALL_FAST_MODEL='us.anthropic.claude-haiku-4-5-20251001-v1:0'",
	}
	for _, w := range want {
		if !strings.Contains(string(data), w) {
			t.Errorf("provider.env missing %q\nGot:\n%s", w, data)
		}
	}
}

func TestApplyProvider_NoClaudeEnvForCopilot(t *testing.T) {
	cwd := t.TempDir()
	_ = os.WriteFile(filepath.Join(cwd, "opencode.json"), []byte("{}"), 0o644)
	p, _ := resolveProvider("copilot")
	applyProvider(p, "", cwd, true)
	if _, err := os.Stat(filepath.Join(cwd, "scripts", "maple", "provider.env")); err == nil {
		t.Error("provider.env should not exist for copilot (no Claude env vars)")
	}
}

func TestPatchAgentDir_TierAssignment(t *testing.T) {
	dir := t.TempDir()
	// Write minimal agent files for each tier
	for _, n := range []string{"orchestrator", "architect", "dotnet", "qa", "docs", "rubber-duck"} {
		body := "---\nname: " + n + "\n---\nbody\n"
		_ = os.WriteFile(filepath.Join(dir, n+".md"), []byte(body), 0o644)
	}
	models := map[providerTier]string{
		tierDeep:  "amazon-bedrock/opus",
		tierFast:  "amazon-bedrock/sonnet",
		tierSmall: "amazon-bedrock/haiku",
	}
	patched, skipped, errored := patchAgentDir(dir, "amazon-bedrock", true, models)
	if errored != 0 {
		t.Errorf("unexpected errors: %d", errored)
	}
	if patched != 6 {
		t.Errorf("expected 6 patched, got %d (skipped=%d)", patched, skipped)
	}

	cases := map[string]string{
		"orchestrator": "amazon-bedrock/opus",
		"architect":    "amazon-bedrock/opus",
		"dotnet":       "amazon-bedrock/sonnet",
		"qa":           "amazon-bedrock/sonnet",
		"docs":         "amazon-bedrock/haiku",
		"rubber-duck":  "amazon-bedrock/haiku",
	}
	for name, want := range cases {
		data, _ := os.ReadFile(filepath.Join(dir, name+".md"))
		if !strings.Contains(string(data), "model: "+want) {
			t.Errorf("%s: expected model %q, file:\n%s", name, want, data)
		}
	}
}
