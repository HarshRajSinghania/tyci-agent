package flowconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the workflow model and role configuration merged from the
// global and project config files. Nothing else reads it yet.
type Config struct {
	Models          map[string]string `json:"models"`
	DefaultModel    string            `json:"default_model"`
	Roles           map[string]Role   `json:"roles"`
	CheckTimeoutSec int               `json:"check_timeout_sec"`
}

// Role describes one workflow role.
type Role struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// defaultCheckTimeout is used when CheckTimeoutSec is 0.
const defaultCheckTimeout = 30 * time.Minute

// defaultPrompt is a hook for embedded default prompts. It currently reports
// no prompt so that only roles defined in config files resolve. The go:embed
// of default prompts is intentionally deferred to the roles issue.
func defaultPrompt(role string) (string, bool) {
	return "", false
}

// Load reads <home>/.tyci/config.json, then <projectDir>/.tyci/config.json
// only when trusted is true. A missing file is not an error. Unknown JSON
// keys are errors. Project scalars win when non-zero, models merge per alias
// and roles are replaced per role name.
func Load(home, projectDir string, trusted bool) (*Config, error) {
	var globalPath, projectPath string
	var globalCfg, projectCfg *Config
	var err error

	if home != "" {
		globalPath = filepath.Join(home, ".tyci", "config.json")
		globalCfg, err = loadFile(globalPath)
		if err != nil {
			return nil, err
		}
	}
	if trusted && projectDir != "" {
		projectPath = filepath.Join(projectDir, ".tyci", "config.json")
		projectCfg, err = loadFile(projectPath)
		if err != nil {
			return nil, err
		}
	}

	merged := &Config{
		Models: map[string]string{},
		Roles:  map[string]Role{},
	}
	roleBaseDir := map[string]string{}
	rolePath := map[string]string{}
	defaultModelPath := ""

	if globalCfg != nil {
		for k, v := range globalCfg.Models {
			merged.Models[k] = v
		}
		globalDir := filepath.Dir(globalPath)
		for name, r := range globalCfg.Roles {
			merged.Roles[name] = r
			roleBaseDir[name] = globalDir
			rolePath[name] = globalPath
		}
		if globalCfg.DefaultModel != "" {
			merged.DefaultModel = globalCfg.DefaultModel
			defaultModelPath = globalPath
		}
		merged.CheckTimeoutSec = globalCfg.CheckTimeoutSec
	}
	if projectCfg != nil {
		for k, v := range projectCfg.Models {
			merged.Models[k] = v
		}
		projectDirBase := filepath.Dir(projectPath)
		for name, r := range projectCfg.Roles {
			merged.Roles[name] = r
			roleBaseDir[name] = projectDirBase
			rolePath[name] = projectPath
		}
		if projectCfg.DefaultModel != "" {
			merged.DefaultModel = projectCfg.DefaultModel
			defaultModelPath = projectPath
		}
		if projectCfg.CheckTimeoutSec != 0 {
			merged.CheckTimeoutSec = projectCfg.CheckTimeoutSec
		}
	}

	if err := resolvePrompts(merged, roleBaseDir, rolePath); err != nil {
		return nil, err
	}
	if err := validate(merged, rolePath, defaultModelPath); err != nil {
		return nil, err
	}
	return merged, nil
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return &Config{
				Models: map[string]string{},
				Roles:  map[string]Role{},
			}, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Models == nil {
		cfg.Models = map[string]string{}
	}
	if cfg.Roles == nil {
		cfg.Roles = map[string]Role{}
	}
	return &cfg, nil
}

func resolvePrompts(cfg *Config, baseDir, cfgPath map[string]string) error {
	for name, r := range cfg.Roles {
		if !strings.HasPrefix(r.Prompt, "@") {
			continue
		}
		ref := strings.TrimPrefix(r.Prompt, "@")
		base := baseDir[name]
		origin := cfgPath[name]
		if ref == "" {
			return fmt.Errorf("%s: role %q has empty prompt file reference", origin, name)
		}
		if filepath.IsAbs(ref) {
			return fmt.Errorf("%s: role %q prompt file %q must be relative, not absolute", origin, name, ref)
		}
		for _, seg := range strings.Split(ref, "/") {
			if seg == ".." {
				return fmt.Errorf("%s: role %q prompt file %q must not contain a dotdot segment", origin, name, ref)
			}
		}
		full := filepath.Join(base, ref)
		fi, err := os.Lstat(full)
		if err != nil {
			return fmt.Errorf("%s: role %q prompt file %q (%s): %w", origin, name, ref, full, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: role %q prompt file %q (%s) must not be a symlink", origin, name, ref, full)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("%s: role %q prompt file %q (%s): %w", origin, name, ref, full, err)
		}
		if len(data) == 0 || len(strings.TrimSpace(string(data))) == 0 {
			return fmt.Errorf("%s: role %q prompt file %q (%s): prompt file is empty", origin, name, ref, full)
		}
		r.Prompt = string(data)
		cfg.Roles[name] = r
	}
	return nil
}

func validate(cfg *Config, rolePath map[string]string, defaultModelPath string) error {
	for name, r := range cfg.Roles {
		if r.Model == "" {
			continue
		}
		if _, ok := cfg.Models[r.Model]; !ok {
			origin := rolePath[name]
			if origin == "" {
				return fmt.Errorf("role %q refers to unknown model %q", name, r.Model)
			}
			return fmt.Errorf("%s: role %q refers to unknown model %q", origin, name, r.Model)
		}
	}
	if cfg.DefaultModel != "" {
		if _, ok := cfg.Models[cfg.DefaultModel]; !ok {
			if defaultModelPath == "" {
				return fmt.Errorf("default_model %q is not defined in models", cfg.DefaultModel)
			}
			return fmt.Errorf("%s: default_model %q is not defined in models", defaultModelPath, cfg.DefaultModel)
		}
	}
	return nil
}

// Role returns the named role or an error when it is not defined. The three
// built-in roles fall back to embedded defaults only when defaultPrompt
// provides one.
func (c *Config) Role(name string) (Role, error) {
	if c == nil {
		return Role{}, fmt.Errorf("role %q is not defined", name)
	}
	if r, ok := c.Roles[name]; ok {
		return r, nil
	}
	switch name {
	case "worker", "review", "merge_decision":
		if prompt, ok := defaultPrompt(name); ok {
			return Role{Prompt: prompt}, nil
		}
	}
	return Role{}, fmt.Errorf("role %q is not defined", name)
}

// ResolveModel returns the provider URI for a role, falling back to the
// default model alias when the role names none.
func (c *Config) ResolveModel(r Role) (string, error) {
	alias := r.Model
	if alias == "" {
		alias = c.DefaultModel
	}
	if alias == "" {
		return "", fmt.Errorf("no model for role and no default_model is set")
	}
	uri, ok := c.Models[alias]
	if !ok {
		return "", fmt.Errorf("unknown model %q", alias)
	}
	return uri, nil
}

// CheckTimeout returns CheckTimeoutSec seconds, or 30 minutes when unset.
func (c *Config) CheckTimeout() time.Duration {
	if c == nil || c.CheckTimeoutSec == 0 {
		return defaultCheckTimeout
	}
	return time.Duration(c.CheckTimeoutSec) * time.Second
}
