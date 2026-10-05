package flowconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeGlobal(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".tyci")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeProject(t *testing.T, projectDir, content string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".tyci")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_MissingFilesIsEmptyConfig(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	cfg, err := Load(home, project, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Models) != 0 {
		t.Fatalf("expected empty models, got %v", cfg.Models)
	}
	if len(cfg.Roles) != 0 {
		t.Fatalf("expected empty roles, got %v", cfg.Roles)
	}
	if cfg.DefaultModel != "" {
		t.Fatalf("expected empty default model, got %q", cfg.DefaultModel)
	}
	if cfg.CheckTimeoutSec != 0 {
		t.Fatalf("expected zero timeout, got %d", cfg.CheckTimeoutSec)
	}
}

func TestLoad_GlobalOnly(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"sonnet": "anthropic://claude-sonnet-5@$ANTHROPIC_API_KEY@api.anthropic.com"},
		"default_model": "sonnet",
		"roles": {"worker": {"model": "sonnet", "prompt": "do work"}},
		"check_timeout_sec": 60
	}`)
	cfg, err := Load(home, project, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Models["sonnet"] == "" {
		t.Fatalf("expected sonnet model, got %v", cfg.Models)
	}
	if cfg.DefaultModel != "sonnet" {
		t.Fatalf("expected default sonnet, got %q", cfg.DefaultModel)
	}
	r, err := cfg.Role("worker")
	if err != nil {
		t.Fatalf("Role: %v", err)
	}
	if r.Prompt != "do work" {
		t.Fatalf("unexpected prompt %q", r.Prompt)
	}
	if got := cfg.CheckTimeout(); got != time.Minute {
		t.Fatalf("expected 1m, got %v", got)
	}
}

func TestLoad_ProjectRoleReplacesGlobalRole(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"m1": "openai://m1@tok@host", "m2": "openai://m2@tok@host"},
		"default_model": "m1",
		"roles": {"worker": {"model": "m1", "prompt": "A"}}
	}`)
	writeProject(t, project, `{
		"roles": {"worker": {"prompt": "B"}}
	}`)
	cfg, err := Load(home, project, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, err := cfg.Role("worker")
	if err != nil {
		t.Fatalf("Role: %v", err)
	}
	if r.Prompt != "B" {
		t.Fatalf("expected prompt B, got %q", r.Prompt)
	}
	if r.Model != "" {
		t.Fatalf("expected empty model after replace, got %q", r.Model)
	}
}

func TestLoad_ModelsMergedPerAlias(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"a": "openai://a@tok@host", "b": "openai://b-global@tok@host"}
	}`)
	writeProject(t, project, `{
		"models": {"b": "openai://b-project@tok@host", "c": "openai://c@tok@host"}
	}`)
	cfg, err := Load(home, project, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Models["a"] != "openai://a@tok@host" {
		t.Fatalf("expected global a kept, got %v", cfg.Models)
	}
	if cfg.Models["b"] != "openai://b-project@tok@host" {
		t.Fatalf("expected project b to win, got %v", cfg.Models)
	}
	if cfg.Models["c"] != "openai://c@tok@host" {
		t.Fatalf("expected project c added, got %v", cfg.Models)
	}
}

func TestLoad_UntrustedIgnoresProjectFile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"m1": "openai://m1@tok@host"},
		"default_model": "m1"
	}`)
	// Make the project config unreadable as a file: a directory at that path.
	// Any open/read attempt would fail, proving trusted=false never opens it.
	projCfgPath := filepath.Join(project, ".tyci", "config.json")
	if err := os.MkdirAll(projCfgPath, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(home, project, false)
	if err != nil {
		t.Fatalf("Load with trusted=false must not open project file: %v", err)
	}
	if cfg.DefaultModel != "m1" {
		t.Fatalf("expected global default, got %q", cfg.DefaultModel)
	}
	if len(cfg.Models) != 1 {
		t.Fatalf("expected only global models, got %v", cfg.Models)
	}
}

func TestLoad_UnknownKeyIsError(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{"rolez": {}}`)
	_, err := Load(home, project, true)
	if err == nil {
		t.Fatal("expected unknown key error")
	}
	globalPath := filepath.Join(home, ".tyci", "config.json")
	if !strings.Contains(err.Error(), globalPath) {
		t.Fatalf("error must contain file path %q, got %v", globalPath, err)
	}
}

func TestLoad_RoleWithUnknownModelIsError(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"m1": "openai://m1@tok@host"},
		"roles": {"worker": {"model": "nope", "prompt": "x"}}
	}`)
	_, err := Load(home, project, true)
	if err == nil {
		t.Fatal("expected unknown model error")
	}
	if !strings.Contains(err.Error(), "worker") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must name role and alias, got %v", err)
	}
}

func TestLoad_DefaultModelWithUnknownAliasIsError(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeGlobal(t, home, `{
		"models": {"m1": "openai://m1@tok@host"},
		"default_model": "nope"
	}`)
	_, err := Load(home, project, true)
	if err == nil {
		t.Fatal("expected unknown default_model error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must name alias, got %v", err)
	}
	globalPath := filepath.Join(home, ".tyci", "config.json")
	if !strings.Contains(err.Error(), globalPath) {
		t.Fatalf("error must contain file path %q, got %v", globalPath, err)
	}
}

func TestLoad_PromptFileReference(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	tyciDir := filepath.Join(home, ".tyci")
	if err := os.MkdirAll(tyciDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tyciDir, "w.md"), []byte("hello prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeGlobal(t, home, `{
		"models": {"m1": "openai://m1@tok@host"},
		"default_model": "m1",
		"roles": {"worker": {"model": "m1", "prompt": "@w.md"}}
	}`)
	cfg, err := Load(home, project, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, err := cfg.Role("worker")
	if err != nil {
		t.Fatalf("Role: %v", err)
	}
	if r.Prompt != "hello prompt" {
		t.Fatalf("expected prompt file content, got %q", r.Prompt)
	}
}

func TestLoad_PromptFileRejected(t *testing.T) {
	cases := []struct {
		name    string
		ref     string
		setup   func(t *testing.T, tyciDir string)
		wantSub string
	}{
		{
			name:    "dotdot",
			ref:     "@../x",
			setup:   func(t *testing.T, tyciDir string) {},
			wantSub: "..",
		},
		{
			name:    "absolute",
			ref:     "@/abs/path",
			setup:   func(t *testing.T, tyciDir string) {},
			wantSub: "absolute",
		},
		{
			name:    "missing",
			ref:     "@missing.md",
			setup:   func(t *testing.T, tyciDir string) {},
			wantSub: "missing.md",
		},
		{
			name: "empty",
			ref:  "@empty.md",
			setup: func(t *testing.T, tyciDir string) {
				if err := os.WriteFile(filepath.Join(tyciDir, "empty.md"), []byte{}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantSub: "prompt file is empty",
		},
		{
			name: "symlink",
			ref:  "@link.md",
			setup: func(t *testing.T, tyciDir string) {
				target := filepath.Join(tyciDir, "real.md")
				if err := os.WriteFile(target, []byte("real"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(tyciDir, "link.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantSub: "symlink",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			project := t.TempDir()
			tyciDir := filepath.Join(home, ".tyci")
			if err := os.MkdirAll(tyciDir, 0o700); err != nil {
				t.Fatal(err)
			}
			tc.setup(t, tyciDir)
			writeGlobal(t, home, `{
				"models": {"m1": "openai://m1@tok@host"},
				"default_model": "m1",
				"roles": {"worker": {"model": "m1", "prompt": "`+tc.ref+`"}}
			}`)
			_, err := Load(home, project, true)
			if err == nil {
				t.Fatalf("expected error for %q", tc.ref)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantSub)) {
				t.Fatalf("error %q must contain %q", err, tc.wantSub)
			}
		})
	}
}

func TestRole_Undefined(t *testing.T) {
	cfg := &Config{
		Models: map[string]string{"m1": "openai://m1@tok@host"},
		Roles:  map[string]Role{},
	}
	_, err := cfg.Role("planner")
	if err == nil {
		t.Fatal("expected undefined role error")
	}
	if !strings.Contains(err.Error(), "planner") {
		t.Fatalf("error must contain role name, got %v", err)
	}
}

func TestResolveModel_FallsBackToDefault(t *testing.T) {
	cfg := &Config{
		Models:       map[string]string{"m1": "uri-1"},
		DefaultModel: "m1",
	}
	got, err := cfg.ResolveModel(Role{})
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != "uri-1" {
		t.Fatalf("expected uri-1, got %q", got)
	}
}

func TestResolveModel_UnknownAlias(t *testing.T) {
	cfg := &Config{
		Models:       map[string]string{"m1": "uri-1"},
		DefaultModel: "m1",
	}
	_, err := cfg.ResolveModel(Role{Model: "nope"})
	if err == nil {
		t.Fatal("expected unknown alias error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error must name alias, got %v", err)
	}
}

func TestResolveModel_NoModelAnywhere(t *testing.T) {
	cfg := &Config{
		Models: map[string]string{"m1": "uri-1"},
	}
	_, err := cfg.ResolveModel(Role{})
	if err == nil {
		t.Fatal("expected no-model error")
	}
}

func TestCheckTimeout_Default(t *testing.T) {
	cfg := &Config{}
	if got := cfg.CheckTimeout(); got != 30*time.Minute {
		t.Fatalf("expected 30m, got %v", got)
	}
	cfg.CheckTimeoutSec = 60
	if got := cfg.CheckTimeout(); got != time.Minute {
		t.Fatalf("expected 1m, got %v", got)
	}
}
