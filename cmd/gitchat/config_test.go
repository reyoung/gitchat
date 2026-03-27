package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reyoung/gitchat/internal/testutil"
)

func TestFindConfigReadsHomeConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	configPath := filepath.Join(root, ".gitchat")
	configBody := "repo: file:///tmp/repo.git\ndb: tmp/cache.db\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, dir, err := findConfig()
	if err != nil {
		t.Fatal(err)
	}
	if dir != root {
		t.Fatalf("expected config dir %s, got %s", root, dir)
	}
	if cfg.Repo != "file:///tmp/repo.git" || cfg.DB != "tmp/cache.db" {
		t.Fatalf("unexpected config %#v", cfg)
	}
}

func TestResolveOptionsUsesConfig(t *testing.T) {
	repoSpec := testutil.NewRemoteRepo(t)
	t.Setenv("GITCHAT_HOME", t.TempDir())
	t.Setenv("USER", "alice")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	root := homeDir
	subdir := filepath.Join(root, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitchat"), []byte("repo: "+repoSpec+"\ndb: cache.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}

	opts, err := resolveOptions(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.RepoSpec != repoSpec {
		t.Fatalf("expected repo spec %s, got %s", repoSpec, opts.RepoSpec)
	}
	wantDB := filepath.Join(root, "cache.db")
	gotEval := normalizePathForTest(opts.DBPath)
	wantEval := normalizePathForTest(wantDB)
	if gotEval != wantEval {
		t.Fatalf("expected db path %s, got %s", wantEval, gotEval)
	}
	if opts.UserName != "alice" {
		t.Fatalf("expected default user alice, got %q", opts.UserName)
	}
	if opts.KeyPath != "" {
		t.Fatalf("expected empty key path when no ~/.ssh key exists in test env, got %q", opts.KeyPath)
	}
}

func TestResolveOptionsUsesConfiguredUserAndKey(t *testing.T) {
	repoSpec := testutil.NewRemoteRepo(t)
	t.Setenv("GITCHAT_HOME", t.TempDir())
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	root := homeDir
	subdir := filepath.Join(root, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "alice.pub")
	if err := os.WriteFile(keyPath, []byte("ssh-ed25519 AAAA test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configBody := "repo: " + repoSpec + "\ndb: cache.db\nuser:\n  name: alice\n  key: alice.pub\n"
	if err := os.WriteFile(filepath.Join(root, ".gitchat"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}

	opts, err := resolveOptions(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.UserName != "alice" {
		t.Fatalf("expected configured user alice, got %q", opts.UserName)
	}
	if normalizePathForTest(opts.KeyPath) != normalizePathForTest(keyPath) {
		t.Fatalf("expected key path %s, got %s", normalizePathForTest(keyPath), normalizePathForTest(opts.KeyPath))
	}
}

func normalizePathForTest(path string) string {
	path = filepath.Clean(path)
	return strings.TrimPrefix(path, "/private")
}
