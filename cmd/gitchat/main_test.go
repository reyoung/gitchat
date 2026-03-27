package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reyoung/gitchat/internal/testutil"
)

func TestCLIWorkflow(t *testing.T) {
	repoSpec := testutil.NewRemoteRepo(t)
	t.Setenv("GITCHAT_HOME", t.TempDir())
	t.Setenv("USER", "alice")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "cache.db")
	if err := os.WriteFile(filepath.Join(homeDir, ".gitchat"), []byte("repo: "+repoSpec+"\ndb: "+dbPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	mustRun := func(args ...string) string {
		t.Helper()
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		runErr := run(context.Background(), args)
		_ = w.Close()
		os.Stdout = oldStdout
		if runErr != nil {
			t.Fatalf("run %v: %v", args, runErr)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(buf.String())
	}

	mustRun("users", "create")
	mustRun("channels", "create", "--channel", "research", "--title", "Research")
	mustRun("messages", "send", "--channel", "research", "--subject", "hello", "--body", "world")
	mustRun("index")

	channelsOut := mustRun("channels", "list")
	if !strings.Contains(channelsOut, "research") {
		t.Fatalf("expected channels output to mention research, got %q", channelsOut)
	}

	messagesOut := mustRun("messages", "list", "--channel", "research")
	if !strings.Contains(messagesOut, "hello") {
		t.Fatalf("expected messages output to mention hello, got %q", messagesOut)
	}
}

func TestCLIWorkflowWithEmptyRemoteBootstrapsMain(t *testing.T) {
	repoSpec := testutil.NewEmptyRemoteRepo(t)
	t.Setenv("GITCHAT_HOME", t.TempDir())
	t.Setenv("USER", "josephyu")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "cache.db")
	if err := os.WriteFile(filepath.Join(homeDir, ".gitchat"), []byte("repo: "+repoSpec+"\ndb: "+dbPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	mustRun := func(args ...string) string {
		t.Helper()
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		runErr := run(context.Background(), args)
		_ = w.Close()
		os.Stdout = oldStdout
		if runErr != nil {
			t.Fatalf("run %v: %v", args, runErr)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(buf.String())
	}

	mustRun("users", "create")
	usersOut := mustRun("users", "list")
	if !strings.Contains(usersOut, "josephyu") {
		t.Fatalf("expected users output to mention josephyu, got %q", usersOut)
	}
}

func TestCLIWorkflowWithLocalRepoConfig(t *testing.T) {
	repoDir := testutil.NewRepo(t)
	t.Setenv("GITCHAT_HOME", t.TempDir())
	t.Setenv("USER", "alice")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "cache.db")
	if err := os.WriteFile(filepath.Join(homeDir, ".gitchat"), []byte("repo: "+repoDir+"\ndb: "+dbPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	mustRun := func(args ...string) string {
		t.Helper()
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		runErr := run(context.Background(), args)
		_ = w.Close()
		os.Stdout = oldStdout
		if runErr != nil {
			t.Fatalf("run %v: %v", args, runErr)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(buf.String())
	}

	mustRun("users", "create")
	mustRun("channels", "create", "--channel", "research", "--title", "Research")
	mustRun("messages", "send", "--channel", "research", "--subject", "hello", "--body", "world")
	mustRun("index")

	messagesOut := mustRun("messages", "list", "--channel", "research")
	if !strings.Contains(messagesOut, "hello") {
		t.Fatalf("expected messages output to mention hello, got %q", messagesOut)
	}
}
