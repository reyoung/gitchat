package testutil

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func NewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.name", "Test User")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, dir, "git", "add", "README.md")
	run(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func NewRemoteRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "git", "init", "--bare", remote)

	work := filepath.Join(root, "seed")
	run(t, root, "git", "clone", remote, work)
	run(t, work, "git", "config", "user.name", "Test User")
	run(t, work, "git", "config", "user.email", "test@example.com")
	run(t, work, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# seed\n"), 0o644); err != nil {
		t.Fatalf("write seed README: %v", err)
	}
	run(t, work, "git", "add", "README.md")
	run(t, work, "git", "commit", "-m", "initial")
	run(t, work, "git", "push", "origin", "HEAD:main")
	return "file://" + remote
}

func NewEmptyRemoteRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "git", "init", "--bare", remote)
	return "file://" + remote
}

func Run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return run(t, dir, args[0], args[1:]...)
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s failed: %v\nstdout=%s\nstderr=%s", name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
