package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/reyoung/gitchat/internal/testutil"
)

func TestBuildCommitMessageStableTrailers(t *testing.T) {
	msg := BuildCommitMessage("subject", "body", map[string]string{
		"Reply-To": "abc",
		"Channel":  "research",
	})
	want := "subject\n\nbody\n\nChannel: research\nReply-To: abc\n"
	if msg != want {
		t.Fatalf("unexpected message:\n%s\nwant:\n%s", msg, want)
	}
}

func TestCleanBodyStripsTrailers(t *testing.T) {
	raw := "line one\n\nChannel: test\nReply-To: abc\n"
	got := cleanBody(raw)
	if got != "line one" {
		t.Fatalf("unexpected body %q", got)
	}
}

func TestParseTrailers(t *testing.T) {
	got := parseTrailers("Channel: test\x1eReply-To: abc")
	if got["Channel"] != "test" || got["Reply-To"] != "abc" {
		t.Fatalf("unexpected trailers %#v", got)
	}
}

func TestMergeOursRetainsSecondParentWithoutChangingTree(t *testing.T) {
	ctx := context.Background()
	dir := testutil.NewRepo(t)
	repo := New(dir)

	mainHead, err := repo.RevParse(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SwitchNewBranch(ctx, "experiments/demo", mainHead); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exp.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "experiment base\n", false); err != nil {
		t.Fatal(err)
	}
	experimentHeadBefore, err := repo.RevParse(ctx, "experiments/demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SwitchNewBranch(ctx, "attempt/demo", experimentHeadBefore); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exp.txt"), []byte("attempt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(ctx, "attempt change\n", false); err != nil {
		t.Fatal(err)
	}
	attemptSHA, err := repo.RevParse(ctx, "attempt/demo")
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.SwitchBranch(ctx, "experiments/demo"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeOurs(ctx, attemptSHA, "retain attempt\n"); err != nil {
		t.Fatal(err)
	}

	rawRepo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	mergeHead, err := rawRepo.ResolveRevision("experiments/demo")
	if err != nil {
		t.Fatal(err)
	}
	mergeCommit, err := rawRepo.CommitObject(*mergeHead)
	if err != nil {
		t.Fatal(err)
	}
	parentCommit, err := rawRepo.CommitObject(mergeCommit.ParentHashes[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(mergeCommit.ParentHashes) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(mergeCommit.ParentHashes))
	}
	if mergeCommit.ParentHashes[1].String() != attemptSHA {
		t.Fatalf("unexpected second parent %s", mergeCommit.ParentHashes[1])
	}
	if mergeCommit.TreeHash != parentCommit.TreeHash {
		t.Fatalf("merge tree changed: got %s want %s", mergeCommit.TreeHash, parentCommit.TreeHash)
	}
}

func TestAnchorCommitToBranchPreservesTreeAndRetainsOrphanMessage(t *testing.T) {
	ctx := context.Background()
	dir := testutil.NewRepo(t)
	repo := New(dir)

	mainHead, err := repo.RevParse(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureBranch(ctx, "users/alice", mainHead); err != nil {
		t.Fatal(err)
	}

	rawRepo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	beforeRef, err := rawRepo.Reference(plumbing.NewBranchReferenceName("users/alice"), false)
	if err != nil {
		t.Fatal(err)
	}
	beforeCommit, err := rawRepo.CommitObject(beforeRef.Hash())
	if err != nil {
		t.Fatal(err)
	}

	messageHash, err := repo.CreateOrphanCommit(ctx, "hello\n", map[string][]byte{
		"attachments/research/demo.txt": []byte("demo\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AnchorCommitToBranch(ctx, "users/alice", messageHash, "anchor\n"); err != nil {
		t.Fatal(err)
	}
	afterRef, err := rawRepo.Reference(plumbing.NewBranchReferenceName("users/alice"), false)
	if err != nil {
		t.Fatal(err)
	}
	afterCommit, err := rawRepo.CommitObject(afterRef.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if len(afterCommit.ParentHashes) != 2 || afterCommit.ParentHashes[0] != beforeCommit.Hash {
		t.Fatalf("unexpected parents: %+v", afterCommit.ParentHashes)
	}
	if afterCommit.ParentHashes[1].String() != messageHash {
		t.Fatalf("unexpected attached orphan parent: %s", afterCommit.ParentHashes[1])
	}
	if afterCommit.TreeHash != beforeCommit.TreeHash {
		t.Fatalf("expected tree hash unchanged, got %s want %s", afterCommit.TreeHash, beforeCommit.TreeHash)
	}
	messageCommit, err := rawRepo.CommitObject(plumbing.NewHash(messageHash))
	if err != nil {
		t.Fatal(err)
	}
	if len(messageCommit.ParentHashes) != 0 {
		t.Fatalf("expected orphan message commit, got parents %+v", messageCommit.ParentHashes)
	}
	msgTree, err := messageCommit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := msgTree.File("attachments/research/demo.txt"); err != nil {
		t.Fatalf("expected orphan message tree to retain attachment: %v", err)
	}
}
