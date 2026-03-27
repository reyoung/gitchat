package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	billyutil "github.com/go-git/go-billy/v5/util"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	idxfmt "github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	repo, err := r.open()
	if err != nil {
		return "", err
	}
	head, err := repo.Storer.Reference(plumbing.HEAD)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return "", nil
		}
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if head.Type() != plumbing.SymbolicReference {
		return "", nil
	}
	target := head.Target()
	if !target.IsBranch() {
		return "", nil
	}
	return target.Short(), nil
}

func Clone(ctx context.Context, repoURL, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("initialize repo %s: %w", dir, err)
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
		Fetch: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	if err != nil && !errors.Is(err, git.ErrRemoteExists) {
		return fmt.Errorf("create origin for %s: %w", repoURL, err)
	}
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Prune:      true,
		Auth:       authForURL(repoURL),
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return fmt.Errorf("fetch origin from %s: %w", repoURL, err)
	}
	return nil
}

func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	repo, err := r.open()
	if err != nil {
		return "", err
	}
	hash, err := resolveRevision(repo, ref)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hash.String(), nil
}

func (r *Repo) BranchExists(ctx context.Context, branch string) bool {
	repo, err := r.open()
	if err != nil {
		return false
	}
	_, err = repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	return err == nil && ctx.Err() == nil
}

func (r *Repo) RefExists(ctx context.Context, ref string) bool {
	repo, err := r.open()
	if err != nil {
		return false
	}
	_, err = repo.Reference(plumbing.ReferenceName(ref), false)
	return err == nil && ctx.Err() == nil
}

func (r *Repo) HasCommit(ctx context.Context) bool {
	repo, err := r.open()
	if err != nil {
		return false
	}
	head, err := repo.Reference(plumbing.HEAD, true)
	return err == nil && !head.Hash().IsZero() && ctx.Err() == nil
}

func (r *Repo) EnsureBranch(ctx context.Context, branch, start string) error {
	if r.BranchExists(ctx, branch) {
		return nil
	}
	repo, err := r.open()
	if err != nil {
		return err
	}
	hash, err := resolveRevision(repo, start)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), hash))
}

func (r *Repo) SwitchBranch(ctx context.Context, branch string) error {
	return r.checkout(ctx, &git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	})
}

func (r *Repo) SwitchNewBranch(ctx context.Context, branch, start string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	hash, err := resolveRevision(repo, start)
	if err != nil {
		return err
	}
	return r.checkout(ctx, &git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Hash:   hash,
		Create: true,
	})
}

func (r *Repo) SwitchTrackBranch(ctx context.Context, branch, remoteRef string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	hash, err := resolveRevision(repo, remoteRef)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), hash)); err != nil {
		return err
	}
	return r.SwitchBranch(ctx, branch)
}

func (r *Repo) SwitchOrphanBranch(ctx context.Context, branch string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	if err := clearFilesystem(r.filesystem()); err != nil {
		return err
	}
	if err := repo.Storer.SetIndex(&idxfmt.Index{Version: 2}); err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) WithSavedBranch(ctx context.Context, fn func() error) error {
	branch, err := r.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		if branch != "" && r.BranchExists(ctx, branch) {
			_ = r.SwitchBranch(ctx, branch)
		}
		return err
	}
	if branch != "" && r.BranchExists(ctx, branch) {
		if err := r.SwitchBranch(ctx, branch); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) WriteFile(relPath string, data []byte) error {
	fs := r.filesystem()
	fullPath := filepath.ToSlash(relPath)
	if err := fs.MkdirAll(filepath.ToSlash(filepath.Dir(fullPath)), 0o755); err != nil {
		return err
	}
	file, err := fs.OpenFile(fullPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func (r *Repo) CopyFile(srcPath, dstRelPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return r.WriteFile(dstRelPath, data)
}

func (r *Repo) AddAll(ctx context.Context) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	status, err := wt.Status()
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		fileStatus := status[path]
		switch {
		case fileStatus.Worktree == git.Deleted || fileStatus.Staging == git.Deleted:
			if _, err := wt.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		default:
			if _, err := wt.Add(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repo) Commit(ctx context.Context, message string, allowEmpty bool) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	sig := defaultSignature(repo)
	_, err = wt.Commit(message, &git.CommitOptions{
		All:               true,
		AllowEmptyCommits: allowEmpty,
		Author:            sig,
		Committer:         sig,
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) AppendCommitToBranch(ctx context.Context, branch, message string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	refName := plumbing.NewBranchReferenceName(branch)
	ref, err := repo.Reference(refName, false)
	if err != nil {
		return err
	}
	parentCommit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	sig := defaultSignature(repo)
	commit := &object.Commit{
		Author:       *sig,
		Committer:    *sig,
		Message:      message,
		TreeHash:     parentCommit.TreeHash,
		ParentHashes: []plumbing.Hash{parentCommit.Hash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, newHash)); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) CreateOrphanCommit(ctx context.Context, message string, files map[string][]byte) (string, error) {
	repo, err := r.open()
	if err != nil {
		return "", err
	}
	treeHash, err := writeTreeWithFiles(repo, plumbing.ZeroHash, files)
	if err != nil {
		return "", err
	}
	sig := defaultSignature(repo)
	commit := &object.Commit{
		Author:    *sig,
		Committer: *sig,
		Message:   message,
		TreeHash:  treeHash,
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return "", err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return "", err
	}
	return newHash.String(), ctx.Err()
}

func (r *Repo) AnchorCommitToBranch(ctx context.Context, branch, commitHash, message string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	refName := plumbing.NewBranchReferenceName(branch)
	ref, err := repo.Reference(refName, false)
	if err != nil {
		return err
	}
	parentCommit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	attachedHash := plumbing.NewHash(strings.TrimSpace(commitHash))
	if attachedHash.IsZero() {
		return fmt.Errorf("commit hash is required")
	}
	if _, err := repo.CommitObject(attachedHash); err != nil {
		return err
	}
	sig := defaultSignature(repo)
	commit := &object.Commit{
		Author:       *sig,
		Committer:    *sig,
		Message:      message,
		TreeHash:     parentCommit.TreeHash,
		ParentHashes: []plumbing.Hash{parentCommit.Hash, attachedHash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, newHash)); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) InitBranchWithFiles(ctx context.Context, branch string, start string, files map[string][]byte, message string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	startHash, err := resolveRevision(repo, start)
	if err != nil {
		return err
	}
	startCommit, err := repo.CommitObject(startHash)
	if err != nil {
		return err
	}
	treeHash, err := writeTreeWithFiles(repo, startCommit.TreeHash, files)
	if err != nil {
		return err
	}
	sig := defaultSignature(repo)
	commit := &object.Commit{
		Author:       *sig,
		Committer:    *sig,
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{startCommit.Hash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	return repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), newHash))
}

func (r *Repo) MergeOurs(ctx context.Context, ref, message string) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	headRef, err := repo.Reference(plumbing.HEAD, true)
	if err != nil {
		return err
	}
	if !headRef.Name().IsBranch() {
		return fmt.Errorf("retain merge requires a checked out branch")
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return err
	}
	retainedHash, err := resolveRevision(repo, ref)
	if err != nil {
		return err
	}
	if _, err := repo.CommitObject(retainedHash); err != nil {
		return err
	}
	sig := defaultSignature(repo)
	commit := &object.Commit{
		Author:       *sig,
		Committer:    *sig,
		Message:      message,
		TreeHash:     headCommit.TreeHash,
		ParentHashes: []plumbing.Hash{headCommit.Hash, retainedHash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(headRef.Name(), newHash)); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) IsReachable(ctx context.Context, ancestor, ref string) (bool, error) {
	repo, err := r.open()
	if err != nil {
		return false, err
	}
	ancestorHash, err := resolveRevision(repo, ancestor)
	if err != nil {
		return false, err
	}
	refHash, err := resolveRevision(repo, ref)
	if err != nil {
		return false, err
	}
	iter, err := repo.Log(&git.LogOptions{From: refHash})
	if err != nil {
		return false, err
	}
	defer iter.Close()
	found := false
	err = iter.ForEach(func(commit *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if commit.Hash == ancestorHash {
			found = true
			return storerStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, storerStop) {
		return false, err
	}
	return found, nil
}

func (r *Repo) Fetch(ctx context.Context, remote string) error {
	if remote == "" {
		remote = "origin"
	}
	repo, err := r.open()
	if err != nil {
		return err
	}
	url, _ := remoteURL(repo, remote)
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: remote,
		Prune:      true,
		Auth:       authForURL(url),
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

func (r *Repo) Push(ctx context.Context, remote, branch string) error {
	if remote == "" {
		remote = "origin"
	}
	repo, err := r.open()
	if err != nil {
		return err
	}
	url, _ := remoteURL(repo, remote)
	refspec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	err = repo.PushContext(ctx, &git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{refspec},
		Auth:       authForURL(url),
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

func (r *Repo) SetRemoteURL(ctx context.Context, remote, repoURL string) error {
	if remote == "" {
		remote = "origin"
	}
	repo, err := r.open()
	if err != nil {
		return err
	}
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	if cfg.Remotes == nil {
		cfg.Remotes = map[string]*config.RemoteConfig{}
	}
	cfg.Remotes[remote] = &config.RemoteConfig{
		Name: remote,
		URLs: []string{repoURL},
		Fetch: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/" + remote + "/*"),
		},
	}
	if err := repo.SetConfig(cfg); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Repo) checkout(ctx context.Context, opts *git.CheckoutOptions) error {
	repo, err := r.open()
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.Checkout(opts); err != nil {
		return err
	}
	return ctx.Err()
}

func clearFilesystem(fs billy.Filesystem) error {
	entries, err := fs.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := billyutil.RemoveAll(fs, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) filesystem() billy.Filesystem {
	if r.memFS != nil {
		return r.memFS
	}
	return osfs.New(r.Dir)
}

func defaultSignature(repo *git.Repository) *object.Signature {
	now := time.Now()
	if cfg, err := repo.ConfigScoped(config.SystemScope); err == nil {
		name := firstNonEmptyString(cfg.Author.Name, cfg.Committer.Name, cfg.User.Name)
		email := firstNonEmptyString(cfg.Author.Email, cfg.Committer.Email, cfg.User.Email)
		if name != "" || email != "" {
			return &object.Signature{
				Name:  firstNonEmptyString(name, "gitchat"),
				Email: firstNonEmptyString(email, "gitchat@local"),
				When:  now,
			}
		}
	}
	return &object.Signature{Name: "gitchat", Email: "gitchat@local", When: now}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func remoteURL(repo *git.Repository, remote string) (string, error) {
	rm, err := repo.Remote(remote)
	if err != nil {
		return "", err
	}
	cfg := rm.Config()
	if len(cfg.URLs) == 0 {
		return "", fmt.Errorf("remote %s has no URLs", remote)
	}
	return cfg.URLs[0], nil
}

func authForURL(raw string) transport.AuthMethod {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	endpoint, err := transport.NewEndpoint(raw)
	if err != nil {
		return nil
	}
	if endpoint.Protocol != "ssh" && endpoint.Protocol != "git+ssh" {
		return nil
	}
	user := endpoint.User
	if user == "" {
		user = "git"
	}
	for _, candidate := range privateKeyCandidates() {
		auth, err := ssh.NewPublicKeysFromFile(user, candidate, "")
		if err == nil {
			return auth
		}
	}
	auth, err := ssh.NewSSHAgentAuth(user)
	if err == nil {
		return auth
	}
	return nil
}

var storerStop = errors.New("stop iteration")

func writeTreeWithFiles(repo *git.Repository, baseTreeHash plumbing.Hash, files map[string][]byte) (plumbing.Hash, error) {
	root := map[string]treeNode{}
	if !baseTreeHash.IsZero() {
		baseTree, err := repo.TreeObject(baseTreeHash)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if err := loadTreeIntoMap(repo, "", baseTree, root); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	for path, data := range files {
		if err := insertBlobAtPath(repo, root, filepath.ToSlash(path), data); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	return writeTreeMap(repo, root)
}

type treeNode struct {
	mode    filemode.FileMode
	hash    plumbing.Hash
	entries map[string]treeNode
}

func loadTreeIntoMap(repo *git.Repository, prefix string, tree *object.Tree, into map[string]treeNode) error {
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir {
			subtree, err := repo.TreeObject(entry.Hash)
			if err != nil {
				return err
			}
			children := map[string]treeNode{}
			if err := loadTreeIntoMap(repo, pathJoin(prefix, entry.Name), subtree, children); err != nil {
				return err
			}
			into[entry.Name] = treeNode{mode: filemode.Dir, entries: children}
			continue
		}
		into[entry.Name] = treeNode{mode: entry.Mode, hash: entry.Hash}
	}
	return nil
}

func insertBlobAtPath(repo *git.Repository, root map[string]treeNode, path string, data []byte) error {
	parts := strings.Split(path, "/")
	current := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			hash, err := writeBlob(repo, data)
			if err != nil {
				return err
			}
			current[part] = treeNode{mode: filemode.Regular, hash: hash}
			return nil
		}
		node, ok := current[part]
		if !ok || node.mode != filemode.Dir || node.entries == nil {
			node = treeNode{mode: filemode.Dir, entries: map[string]treeNode{}}
		}
		current[part] = node
		current = node.entries
	}
	return nil
}

func writeBlob(repo *git.Repository, data []byte) (plumbing.Hash, error) {
	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

func writeTreeMap(repo *git.Repository, entries map[string]treeNode) (plumbing.Hash, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	tree := &object.Tree{Entries: make([]object.TreeEntry, 0, len(names))}
	for _, name := range names {
		node := entries[name]
		hash := node.hash
		if node.mode == filemode.Dir {
			var err error
			hash, err = writeTreeMap(repo, node.entries)
			if err != nil {
				return plumbing.ZeroHash, err
			}
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{
			Name: name,
			Mode: node.mode,
			Hash: hash,
		})
	}
	obj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

func pathJoin(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "/" + child
}

func privateKeyCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")
	candidates := []string{
		filepath.Join(sshDir, "id_ed25519"),
		filepath.Join(sshDir, "id_rsa"),
		filepath.Join(sshDir, "id_ecdsa"),
		filepath.Join(sshDir, "id_dsa"),
	}
	for _, name := range []string{"identity", "git_identity"} {
		candidates = append(candidates, filepath.Join(sshDir, name))
	}
	entries, err := os.ReadDir(sshDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".pub") || strings.HasSuffix(name, ".pem") || strings.HasPrefix(name, "known_hosts") || strings.HasPrefix(name, "config") {
				continue
			}
			candidates = append(candidates, filepath.Join(sshDir, name))
		}
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			out = append(out, candidate)
		}
	}
	return out
}

func BuildCommitMessage(subject, body string, trailers map[string]string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(subject))
	b.WriteString("\n\n")
	if strings.TrimSpace(body) != "" {
		b.WriteString(strings.TrimSpace(body))
		b.WriteString("\n\n")
	}
	keys := make([]string, 0, len(trailers))
	for key := range trailers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := trailers[key]
		if strings.TrimSpace(value) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("%s: %s\n", key, value))
	}
	return strings.TrimSpace(b.String()) + "\n"
}
