package main

// Provider presets — mirrors pi-coding-agent's `--provider <name>` flag.
//
// Each preset bundles:
//   - The opencode.json provider block (options + auth references)
//   - Model IDs for three tiers (deep / fast / small)
//   - Optional Claude Code env vars (written to scripts/maple/provider.env)
//   - A post-install hint printed after `maple init --provider <name>`
//
// Apply phase (`applyProvider`) patches three things in the user's project:
//   1. `opencode.json`     — merges the provider block + sets top-level `model`
//   2. `.opencode/agents/*.md` + `.claude/agents/*.md` — injects `model:` frontmatter per tier
//   3. `scripts/maple/provider.env` — Claude Code env vars (when relevant)
//
// All steps are idempotent and additive — an agent file that already has a
// `model:` line in its frontmatter is left untouched (respects user overrides).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type providerTier string

const (
	tierDeep  providerTier = "deep"  // orchestrator, architect
	tierFast  providerTier = "fast"  // default for implementation agents
	tierSmall providerTier = "small" // docs, rubber-duck, lightweight reviewers
)

// agentTierMap maps agent file basename (without .md) to a tier.
// Anything not listed falls through to tierFast.
var agentTierMap = map[string]providerTier{
	"orchestrator": tierDeep,
	"architect":    tierDeep,

	"docs":        tierSmall,
	"rubber-duck": tierSmall,
	"humanizer":   tierSmall,
	"spec-kit":    tierSmall,
}

type providerPreset struct {
	Name          string                  // canonical opencode provider key
	Aliases       []string                // CLI-accepted aliases (include Name itself)
	Models        map[providerTier]string // values include the "<provider>/" prefix used by opencode
	OpenCodeBlock map[string]any          // value placed at provider.<Name> in opencode.json
	ClaudeEnv     map[string]string       // static env vars written to scripts/maple/provider.env
	// ClaudeEnvFromModel maps an env var name to the tier whose model ID supplies
	// its value (provider prefix stripped). Derived at write time so the env file
	// never drifts from Models and honours --model overrides.
	ClaudeEnvFromModel map[string]providerTier
	ClaudeNative       bool   // true ⇢ Claude Code can talk to this provider directly
	SetupHint          string // printed after init
}

var providerPresets = []providerPreset{
	{
		Name:    "anthropic",
		Aliases: []string{"anthropic", "claude"},
		Models: map[providerTier]string{
			tierDeep:  "anthropic/claude-opus-4-5",
			tierFast:  "anthropic/claude-sonnet-4-5",
			tierSmall: "anthropic/claude-3-5-haiku-latest",
		},
		OpenCodeBlock: map[string]any{
			"apiKey": "{env:ANTHROPIC_API_KEY}",
		},
		ClaudeNative: true,
		SetupHint: `Anthropic API selected.
  export ANTHROPIC_API_KEY=sk-ant-...
  (Claude Code uses this automatically — no extra config.)`,
	},
	{
		Name:    "openai",
		Aliases: []string{"openai", "oai"},
		Models: map[providerTier]string{
			tierDeep:  "openai/gpt-5",
			tierFast:  "openai/gpt-5-mini",
			tierSmall: "openai/gpt-4.1-mini",
		},
		OpenCodeBlock: map[string]any{
			"apiKey": "{env:OPENAI_API_KEY}",
		},
		ClaudeNative: false,
		SetupHint: `OpenAI selected.
  export OPENAI_API_KEY=sk-...
  Note: Claude Code itself cannot route to OpenAI — use 'opencode .' as the harness.`,
	},
	{
		Name:    "github-copilot",
		Aliases: []string{"github-copilot", "copilot", "gh-copilot"},
		Models: map[providerTier]string{
			tierDeep:  "github-copilot/claude-opus-4",
			tierFast:  "github-copilot/claude-sonnet-4.5",
			tierSmall: "github-copilot/gpt-4.1",
		},
		OpenCodeBlock: map[string]any{},
		ClaudeNative:  false,
		SetupHint: `GitHub Copilot selected.
  gh auth login --scopes copilot
  Note: route through OpenCode — Claude Code cannot use Copilot endpoints directly.`,
	},
	{
		Name:    "amazon-bedrock",
		Aliases: []string{"amazon-bedrock", "bedrock", "aws-bedrock", "aws"},
		Models: map[providerTier]string{
			tierDeep:  "amazon-bedrock/us.anthropic.claude-opus-4-8",
			tierFast:  "amazon-bedrock/us.anthropic.claude-sonnet-4-6",
			tierSmall: "amazon-bedrock/us.anthropic.claude-haiku-4-5-20251001-v1:0",
		},
		OpenCodeBlock: map[string]any{
			"options": map[string]any{
				"region": "{env:AWS_REGION}",
			},
		},
		ClaudeNative: true,
		ClaudeEnv: map[string]string{
			"CLAUDE_CODE_USE_BEDROCK": "1",
			"AWS_REGION":              "us-east-1",
		},
		ClaudeEnvFromModel: map[string]providerTier{
			"ANTHROPIC_MODEL":            tierFast,
			"ANTHROPIC_SMALL_FAST_MODEL": tierSmall,
		},
		SetupHint: `AWS Bedrock selected.
  1. Enable model access: AWS Console → Bedrock → Model access
  2. Export credentials (any of):
       export AWS_PROFILE=your-profile
       # or
       export AWS_ACCESS_KEY_ID=... ; export AWS_SECRET_ACCESS_KEY=...
  3. For Claude Code, also source:
       source scripts/maple/provider.env
  See docs/quickstart-bedrock.md for IAM, model IDs, and troubleshooting.`,
	},
}

// resolveProvider matches by name or alias (case-insensitive).
func resolveProvider(name string) (*providerPreset, error) {
	if name == "" {
		return nil, nil
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for i := range providerPresets {
		p := &providerPresets[i]
		for _, a := range p.Aliases {
			if a == want {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("unknown provider %q — supported: %s", name, providerNamesList())
}

func providerNamesList() string {
	names := make([]string, 0, len(providerPresets))
	for _, p := range providerPresets {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// applyProvider runs after the template copy in doInit.
// modelOverride (if non-empty) replaces every tier with that single model ID.
// explicit is true when the user passed --provider on this run (a deliberate
// switch) vs. false on a `maple update` replay of the persisted choice. On an
// explicit switch any MAPLE-managed model: line is rewritten to the new provider;
// on a replay only this provider's own lines are touched, so an agent the user
// hand-pointed at a different provider survives.
// Returns log lines (✓ / ↺ / ~) for the init wizard.
func applyProvider(p *providerPreset, modelOverride, cwd string, explicit bool) []string {
	var logs []string
	log := func(s string) { logs = append(logs, s) }

	// 1. Build the per-tier model resolution. Override collapses all tiers.
	models := map[providerTier]string{
		tierDeep:  p.Models[tierDeep],
		tierFast:  p.Models[tierFast],
		tierSmall: p.Models[tierSmall],
	}
	if modelOverride != "" {
		models[tierDeep] = modelOverride
		models[tierFast] = modelOverride
		models[tierSmall] = modelOverride
		log(fmt.Sprintf("✓ provider %s — model override: %s", p.Name, modelOverride))
	} else {
		log(fmt.Sprintf("✓ provider %s (deep=%s, fast=%s, small=%s)",
			p.Name, models[tierDeep], models[tierFast], models[tierSmall]))
	}

	// 2. Patch opencode.json (best-effort — log error, don't abort init).
	switch err := patchOpencodeJSON(filepath.Join(cwd, "opencode.json"), p, models[tierFast]); {
	case os.IsNotExist(err):
		log("~ opencode.json not present, skipped (Claude Code only)")
	case err != nil:
		log("~ opencode.json: " + err.Error())
	default:
		log("↺ opencode.json (provider + default model)")
	}

	// 3. Inject `model:` frontmatter into agent files for both harnesses.
	for _, dir := range []string{".opencode/agents", ".claude/agents"} {
		full := filepath.Join(cwd, dir)
		stat, err := os.Stat(full)
		if err != nil || !stat.IsDir() {
			continue
		}
		patched, skipped, errs := patchAgentDir(full, p.Name, explicit, models)
		log(fmt.Sprintf("↺ %s/  (%d patched, %d kept user-set, %d error)", dir, patched, skipped, errs))
	}

	// 4. Write Claude Code env file when the provider needs it.
	if len(p.ClaudeEnv) > 0 || len(p.ClaudeEnvFromModel) > 0 {
		envPath := filepath.Join(cwd, "scripts", "maple", "provider.env")
		if err := writeClaudeEnv(envPath, p, models); err != nil {
			log("~ scripts/maple/provider.env: " + err.Error())
		} else {
			log("✓ scripts/maple/provider.env (source before launching `claude`)")
		}
	}

	// 5. Persist the choice so `maple update` can re-apply without the flag.
	if err := writeProviderState(filepath.Join(cwd, ".maple", "provider.json"), p.Name, modelOverride); err != nil {
		log("~ .maple/provider.json: " + err.Error())
	}

	return logs
}

// reqProviderRuntime returns the persisted provider choice resolved for the
// inline `maple req` LLM call: env entries to layer onto the child process and
// the model ID to target (deep tier, or the --model override if one was pinned).
// Returns (nil, nil, "") when no provider is configured, so the caller falls
// back to the bare harness defaults.
//
// existing is the current process environment (os.Environ()); static ClaudeEnv
// keys already present there are left as-is so a user's exported AWS_REGION wins.
func reqProviderRuntime(cwd string, existing []string) (preset *providerPreset, env []string, model string) {
	name, override := loadProviderState(cwd)
	if name == "" {
		return nil, nil, ""
	}
	p, err := resolveProvider(name)
	if err != nil || p == nil {
		return nil, nil, ""
	}

	models := map[providerTier]string{
		tierDeep:  p.Models[tierDeep],
		tierFast:  p.Models[tierFast],
		tierSmall: p.Models[tierSmall],
	}
	if override != "" {
		models[tierDeep], models[tierFast], models[tierSmall] = override, override, override
	}

	have := make(map[string]bool, len(existing))
	for _, e := range existing {
		if i := strings.Index(e, "="); i >= 0 {
			have[e[:i]] = true
		}
	}
	for k, v := range p.ClaudeEnv {
		if !have[k] {
			env = append(env, k+"="+v)
		}
	}
	for k, tier := range p.ClaudeEnvFromModel {
		env = append(env, k+"="+stripProviderPrefix(models[tier]))
	}
	return p, env, models[tierDeep]
}

// loadProviderState reads the persisted `maple init --provider` choice, if any.
// Used by `maple update` so the user doesn't have to re-pass the flag.
func loadProviderState(cwd string) (provider, modelOverride string) {
	data, err := os.ReadFile(filepath.Join(cwd, ".maple", "provider.json"))
	if err != nil {
		return "", ""
	}
	var s struct {
		Provider string `json:"provider"`
		Model    string `json:"model_override"`
	}
	if json.Unmarshal(data, &s) != nil {
		return "", ""
	}
	return s.Provider, s.Model
}

func writeProviderState(path, name, modelOverride string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := map[string]string{
		"provider":       name,
		"model_override": modelOverride,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// patchOpencodeJSON merges the provider block + sets the top-level default model.
// Preserves every other field that the user (or template) already wrote.
func patchOpencodeJSON(path string, p *providerPreset, defaultModel string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// provider.<name> = block
	provBlock, _ := doc["provider"].(map[string]any)
	if provBlock == nil {
		provBlock = map[string]any{}
	}
	// Only overwrite our own provider key; leave any other providers (user added) alone.
	provBlock[p.Name] = p.OpenCodeBlock
	doc["provider"] = provBlock

	// Top-level default model
	doc["model"] = defaultModel

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// patchAgentDir walks every *.md and injects `model:` into the YAML frontmatter
// based on agentTierMap. Returns counts: patched, skipped (already had model:), errored.
func patchAgentDir(dir, provider string, explicit bool, models map[providerTier]string) (patched, skipped, errored int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, 1
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		tier, ok := agentTierMap[name]
		if !ok {
			tier = tierFast
		}
		model := models[tier]
		path := filepath.Join(dir, e.Name())
		changed, err := injectModelFrontmatter(path, model, provider, explicit)
		switch {
		case err != nil:
			errored++
		case changed:
			patched++
		default:
			skipped++
		}
	}
	return
}

// knownProviderPrefixes returns the `<name>/` prefixes used by all presets.
// A `model:` value starting with one of these is considered MAPLE-managed
// and may be overwritten on subsequent `maple init --provider X` runs.
// Hand-set values (anything else) are preserved.
func knownProviderPrefixes() []string {
	out := make([]string, 0, len(providerPresets))
	for _, p := range providerPresets {
		out = append(out, p.Name+"/")
	}
	return out
}

// injectModelFrontmatter inserts `model: <id>` into the agent file's YAML
// frontmatter, idempotently. Returns (changed, err).
//
// Rules:
//   - File must start with `---` on a line by itself.
//   - If the frontmatter already has `model:`, it is overwritable only when the
//     value is MAPLE-managed. On an explicit `--provider` switch that means any
//     known-provider prefix; on a replay (explicit=false) it means only the
//     currently-selected provider's prefix — so an agent the user hand-pointed
//     at a different provider survives `maple update`.
//   - If the value is not overwritable (user-customized, or another provider on
//     a replay), leave it alone.
//   - Otherwise insert `model: <id>` right before the closing `---`.
func injectModelFrontmatter(path, model, provider string, explicit bool) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return false, nil // not a frontmatter file — skip silently
	}

	// Find the closing ---
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return false, nil
	}

	// On an explicit switch, any known-provider prefix is overwritable; on a
	// replay, only the currently-selected provider's own prefix is.
	var overwritable []string
	if explicit {
		overwritable = knownProviderPrefixes()
	} else {
		overwritable = []string{provider + "/"}
	}

	// Look for an existing model: line inside the frontmatter.
	for i := 1; i < closeIdx; i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if !strings.HasPrefix(trimmed, "model:") {
			continue
		}
		curVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "model:"))
		isManaged := false
		for _, pfx := range overwritable {
			if strings.HasPrefix(curVal, pfx) {
				isManaged = true
				break
			}
		}
		if !isManaged {
			return false, nil // user-customized or another provider on replay — preserve
		}
		if curVal == model {
			return false, nil // already correct
		}
		lines[i] = "model: " + model
		return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}

	// No model: line yet — insert before the closing ---.
	newLine := "model: " + model
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:closeIdx]...)
	out = append(out, newLine)
	out = append(out, lines[closeIdx:]...)
	return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// writeClaudeEnv renders scripts/maple/provider.env. Static values come from
// p.ClaudeEnv; model-derived values come from the resolved per-tier models
// (provider prefix stripped) so the file honours --model and never drifts from
// the preset's Models map. AWS_REGION keeps a ${AWS_REGION:-default} escape hatch
// so sourcing the file does not clobber a region the user already exported.
func writeClaudeEnv(path string, p *providerPreset, models map[providerTier]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	vals := make(map[string]string, len(p.ClaudeEnv)+len(p.ClaudeEnvFromModel))
	for k, v := range p.ClaudeEnv {
		vals[k] = v
	}
	for k, tier := range p.ClaudeEnvFromModel {
		vals[k] = stripProviderPrefix(models[tier])
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# Generated by `maple init --provider " + p.Name + "`.\n")
	b.WriteString("# Source this file before launching `claude`:\n")
	b.WriteString("#   source scripts/maple/provider.env\n\n")
	for _, k := range keys {
		if k == "AWS_REGION" {
			b.WriteString("export AWS_REGION=${AWS_REGION:-" + vals[k] + "}\n")
			continue
		}
		b.WriteString("export " + k + "=" + shellQuote(vals[k]) + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// stripProviderPrefix removes a leading "<provider>/" from a model ID, leaving
// the bare model name that Claude Code's ANTHROPIC_MODEL-style vars expect.
func stripProviderPrefix(model string) string {
	if i := strings.Index(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}

func shellQuote(s string) string {
	// Single-quote unless the value is alphanumeric/safe — keeps the file
	// re-source-able regardless of model-ID characters like ':' and '.'.
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}
