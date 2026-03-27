package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	osuser "os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type cliConfig struct {
	Repo string `yaml:"repo"`
	DB   string `yaml:"db"`
	User struct {
		Name string `yaml:"name"`
		Key  string `yaml:"key"`
	} `yaml:"user"`
}

type resolvedOptions struct {
	RepoSpec      string
	DBPath        string
	UserName      string
	KeyPath       string
}

func resolveOptions(ctx context.Context, repoArg, dbArg string) (resolvedOptions, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return resolvedOptions{}, err
	}
	cfg, cfgDir, err := findConfig()
	if err != nil {
		return resolvedOptions{}, err
	}

	repoSpec := firstNonEmpty(strings.TrimSpace(repoArg), strings.TrimSpace(cfg.Repo))
	if repoSpec == "" {
		return resolvedOptions{}, fmt.Errorf("repo is required; pass --repo or define it in ~/.gitchat")
	}
	repoSpec, err = resolveRepoSpec(ctx, repoSpec)
	if err != nil {
		return resolvedOptions{}, err
	}

	dbPath := strings.TrimSpace(dbArg)
	if dbPath == "" {
		dbPath = strings.TrimSpace(cfg.DB)
		if dbPath != "" {
			dbPath = expandPath(dbPath, cfgDir)
		}
	}
	if dbPath == "" {
		dbPath = defaultDBPath(repoSpec)
	} else if !filepath.IsAbs(dbPath) {
		dbPath = expandPath(dbPath, cwd)
	}

	return resolvedOptions{
		RepoSpec:      repoSpec,
		DBPath:        dbPath,
		UserName:      resolveDefaultUserName(cfg),
		KeyPath:       resolveDefaultKeyPath(cfg, cfgDir),
	}, nil
}

func findConfig() (cliConfig, string, error) {
	home := currentHomeDir()
	if home == "" {
		return cliConfig{}, "", fmt.Errorf("cannot determine home directory")
	}
	path := filepath.Join(home, ".gitchat")
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return cliConfig{}, "", err
		}
		var cfg cliConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cliConfig{}, "", fmt.Errorf("parse %s: %w", path, err)
		}
		return cfg, home, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return cliConfig{}, "", err
	}
	return cliConfig{}, home, nil
}

func expandPath(pathValue, baseDir string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return ""
	}
	if strings.HasPrefix(pathValue, "~/") || pathValue == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if pathValue == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(pathValue, "~/"))
		}
	}
	if filepath.IsAbs(pathValue) {
		return pathValue
	}
	return filepath.Join(baseDir, pathValue)
}

func resolveUserName(arg string, opts resolvedOptions) string {
	return firstNonEmpty(strings.TrimSpace(arg), opts.UserName)
}

func resolveKeyPath(arg string, opts resolvedOptions) string {
	if strings.TrimSpace(arg) != "" {
		return expandPath(arg, mustGetwd())
	}
	return opts.KeyPath
}

func resolveDefaultUserName(cfg cliConfig) string {
	if value := strings.TrimSpace(cfg.User.Name); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	current, err := osuser.Current()
	if err == nil && strings.TrimSpace(current.Username) != "" {
		return strings.TrimSpace(current.Username)
	}
	return ""
}

func resolveDefaultKeyPath(cfg cliConfig, cfgDir string) string {
	if value := strings.TrimSpace(cfg.User.Key); value != "" {
		return expandPath(value, cfgDir)
	}
	home := currentHomeDir()
	if home == "" {
		return ""
	}
	sshDir := filepath.Join(home, ".ssh")
	candidates := []string{
		filepath.Join(sshDir, "id_ed25519.pub"),
		filepath.Join(sshDir, "id_rsa.pub"),
		filepath.Join(sshDir, "id_ecdsa.pub"),
		filepath.Join(sshDir, "id_dsa.pub"),
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		return filepath.Join(sshDir, entry.Name())
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func currentHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func repoCacheDir() string {
	if custom := strings.TrimSpace(os.Getenv("GITCHAT_HOME")); custom != "" {
		return custom
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(currentHomeDir(), ".cache", "gitchat")
	}
	return filepath.Join(base, "gitchat")
}

func defaultDBPath(repoSpec string) string {
	hash := sha256.Sum256([]byte(repoSpec))
	return filepath.Join(repoCacheDir(), "db", hex.EncodeToString(hash[:16])+".sqlite")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
