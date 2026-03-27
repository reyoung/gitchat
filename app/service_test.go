package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/reyoung/gitchat/gitrepo"
	"github.com/reyoung/gitchat/internal/testutil"
	"github.com/reyoung/gitchat/store"
)

func TestServiceWorkflow(t *testing.T) {
	ctx := context.Background()
	repoDir := testutil.NewRepo(t)
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := NewService(gitrepo.New(repoDir), st)
	svc.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	if err := svc.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateUser(ctx, "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateUser(ctx, "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateChannel(ctx, "research", "alice", "Research"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddChannelMember(ctx, "research", "alice", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateExperiment(ctx, "exp1", "alice", "Experiment One", "main", `{"prompt":"hi"}`); err != nil {
		t.Fatal(err)
	}

	testutil.Run(t, repoDir, "git", "switch", "experiments/exp1")
	preRetainTree := testutil.Run(t, repoDir, "git", "rev-parse", "HEAD^{tree}")
	testutil.Run(t, repoDir, "git", "switch", "-c", "attempt/exp1")
	if err := svc.Repo.WriteFile("attempt.txt", []byte("attempt\n")); err != nil {
		t.Fatal(err)
	}
	testutil.Run(t, repoDir, "git", "add", "attempt.txt")
	testutil.Run(t, repoDir, "git", "commit", "-m", "attempt commit")
	attemptSHA := testutil.Run(t, repoDir, "git", "rev-parse", "HEAD")
	testutil.Run(t, repoDir, "git", "switch", "main")

	if err := svc.RetainExperimentAttempt(ctx, "exp1", attemptSHA); err != nil {
		t.Fatal(err)
	}
	postRetainTree := testutil.Run(t, repoDir, "git", "rev-parse", "experiments/exp1^{tree}")
	if preRetainTree != postRetainTree {
		t.Fatalf("retain merge changed tree: before=%s after=%s", preRetainTree, postRetainTree)
	}

	if err := svc.SendMessage(ctx, SendMessageInput{
		UserID:        "alice",
		ChannelID:     "research",
		Subject:       "hello",
		Body:          "world",
		ExperimentID:  "exp1",
		ExperimentSHA: attemptSHA,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SendMessage(ctx, SendMessageInput{
		UserID:    "bob",
		ChannelID: "research",
		Subject:   "reply",
		Body:      "ok",
	}); err != nil {
		t.Fatal(err)
	}

	messages, err := st.ListMessagesByChannel(ctx, "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	rawRepo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var helloMsg, replyMsg *modelMessageView
	for i := range messages {
		switch messages[i].Subject {
		case "hello":
			helloMsg = &modelMessageView{idx: i}
		case "reply":
			replyMsg = &modelMessageView{idx: i}
		}
	}
	if helloMsg == nil || replyMsg == nil {
		t.Fatalf("expected hello and reply messages, got %#v", messages)
	}
	if messages[helloMsg.idx].ExperimentID != "exp1" || messages[helloMsg.idx].ExperimentSHA != attemptSHA {
		t.Fatalf("expected experiment reference on hello message: %#v", messages[helloMsg.idx])
	}
	if len(messages[replyMsg.idx].Follows) != 1 || messages[replyMsg.idx].Follows[0] != messages[helloMsg.idx].CommitHash {
		t.Fatalf("expected reply message to follow hello, got %#v", messages[replyMsg.idx].Follows)
	}
	helloCommit, err := rawRepo.CommitObject(plumbing.NewHash(messages[helloMsg.idx].CommitHash))
	if err != nil {
		t.Fatal(err)
	}
	if len(helloCommit.ParentHashes) != 0 {
		t.Fatalf("expected hello message to be an orphan commit, got parents %+v", helloCommit.ParentHashes)
	}
	replyCommit, err := rawRepo.CommitObject(plumbing.NewHash(messages[replyMsg.idx].CommitHash))
	if err != nil {
		t.Fatal(err)
	}
	if len(replyCommit.ParentHashes) != 0 {
		t.Fatalf("expected reply message to be an orphan commit, got parents %+v", replyCommit.ParentHashes)
	}

	heads, err := svc.ChannelHeads(ctx, "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 || heads[0] != messages[replyMsg.idx].CommitHash {
		t.Fatalf("unexpected channel heads: %#v", heads)
	}

	channels, err := st.ListChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ID != "research" {
		t.Fatalf("unexpected channels: %#v", channels)
	}

	experiments, err := st.ListExperiments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(experiments) != 1 || experiments[0].ID != "exp1" {
		t.Fatalf("unexpected experiments: %#v", experiments)
	}

	reachable := testutil.Run(t, repoDir, "git", "merge-base", "--is-ancestor", attemptSHA, "experiments/exp1")
	if strings.TrimSpace(reachable) != "" {
		t.Fatalf("expected merge-base ancestor check to be silent, got %q", reachable)
	}
}

func TestSendMessageAutoCreatesMissingUser(t *testing.T) {
	ctx := context.Background()
	repoDir := testutil.NewRepo(t)
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := NewService(gitrepo.New(repoDir), st)
	svc.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	if err := svc.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateUser(ctx, "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateChannel(ctx, "research", "alice", "Research"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddChannelMember(ctx, "research", "alice", "carol"); err != nil {
		t.Fatal(err)
	}

	if err := svc.SendMessage(ctx, SendMessageInput{
		UserID:    "carol",
		ChannelID: "research",
		Subject:   "hello",
		Body:      "auto user",
	}); err != nil {
		t.Fatal(err)
	}

	users, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, user := range users {
		if user.ID == "carol" && user.Branch == "users/carol" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected carol to be auto-created, got %#v", users)
	}

	rawRepo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	userRef, err := rawRepo.Reference(plumbing.NewBranchReferenceName("users/carol"), false)
	if err != nil {
		t.Fatal(err)
	}
	headCommit, err := rawRepo.CommitObject(userRef.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if len(headCommit.ParentHashes) != 2 {
		t.Fatalf("expected user branch head to be an anchor merge, got %+v", headCommit.ParentHashes)
	}
	messageCommit, err := rawRepo.CommitObject(headCommit.ParentHashes[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(messageCommit.ParentHashes) != 0 {
		t.Fatalf("expected attached message commit to be orphan, got %+v", messageCommit.ParentHashes)
	}
}

type modelMessageView struct {
	idx int
}
