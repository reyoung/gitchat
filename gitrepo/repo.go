package gitrepo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/reyoung/gitchat/model"
)

type Repo struct {
	Dir       string
	RemoteURL string
	memRepo   *git.Repository
	memFS     billy.Filesystem
}

func New(dir string) *Repo {
	return &Repo{Dir: dir}
}

func NewRemote(remoteURL string) *Repo {
	return &Repo{RemoteURL: remoteURL}
}

func (r *Repo) open() (*git.Repository, error) {
	if strings.TrimSpace(r.RemoteURL) != "" {
		return r.openMemory()
	}
	repo, err := git.PlainOpen(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("open repo %s: %w", r.Dir, err)
	}
	return repo, nil
}

func (r *Repo) openMemory() (*git.Repository, error) {
	if r.memRepo != nil {
		return r.memRepo, nil
	}
	fs := memfs.New()
	repo, err := git.Init(memory.NewStorage(), fs)
	if err != nil {
		return nil, fmt.Errorf("init memory repo: %w", err)
	}
	if strings.TrimSpace(r.RemoteURL) != "" {
		_, err = repo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{r.RemoteURL},
			Fetch: []config.RefSpec{
				config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
			},
		})
		if err != nil && err != git.ErrRemoteExists {
			return nil, err
		}
	}
	r.memRepo = repo
	r.memFS = fs
	return repo, nil
}

func (r *Repo) ListRefs(ctx context.Context) ([]model.RefState, error) {
	repo, err := r.open()
	if err != nil {
		return nil, err
	}
	iter, err := repo.References()
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	refs := make([]model.RefState, 0)
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := ref.Name().String()
		if !strings.HasPrefix(name, "refs/") {
			return nil
		}
		resolved := ref
		if ref.Type() != plumbing.HashReference {
			var err error
			resolved, err = repo.Reference(ref.Name(), true)
			if err != nil {
				return nil
			}
		}
		if resolved.Type() != plumbing.HashReference || resolved.Hash().IsZero() {
			return nil
		}
		refs = append(refs, model.RefState{
			Name:     ref.Name().Short(),
			HeadHash: resolved.Hash().String(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Name < refs[j].Name
	})
	return refs, nil
}

type Commit struct {
	Hash       string
	ParentLine string
	Subject    string
	Body       string
	CommitTime time.Time
	Trailers   map[string]string
}

func (r *Repo) ListCommits(ctx context.Context, ref string) ([]Commit, error) {
	repo, err := r.open()
	if err != nil {
		return nil, err
	}
	hash, err := resolveRevision(repo, r.resolveRef(ref))
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&git.LogOptions{From: hash})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	commits := make([]Commit, 0)
	err = iter.ForEach(func(c *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		subject, body, trailers := splitCommitMessage(c.Message)
		parents := make([]string, 0, len(c.ParentHashes))
		for _, parent := range c.ParentHashes {
			parents = append(parents, parent.String())
		}
		commits = append(commits, Commit{
			Hash:       c.Hash.String(),
			ParentLine: strings.Join(parents, " "),
			Subject:    subject,
			Body:       body,
			CommitTime: c.Committer.When,
			Trailers:   trailers,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

func (r *Repo) resolveRef(ref string) string {
	if strings.HasPrefix(ref, "refs/") {
		return ref
	}
	if strings.HasPrefix(ref, "origin/") {
		return "refs/remotes/" + ref
	}
	if strings.Contains(ref, "/") {
		return "refs/heads/" + ref
	}
	return ref
}

func splitCommitMessage(raw string) (string, string, map[string]string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return "", "", map[string]string{}
	}
	lines := strings.Split(raw, "\n")
	subject := strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		return subject, "", map[string]string{}
	}
	bodyLines := lines[1:]
	for len(bodyLines) > 0 && strings.TrimSpace(bodyLines[0]) == "" {
		bodyLines = bodyLines[1:]
	}
	body, trailers := splitBodyAndTrailers(bodyLines)
	return subject, cleanBody(strings.Join(body, "\n")), trailers
}

func splitBodyAndTrailers(lines []string) ([]string, map[string]string) {
	trailers := map[string]string{}
	if len(lines) == 0 {
		return nil, trailers
	}
	i := len(lines) - 1
	for i >= 0 && strings.TrimSpace(lines[i]) == "" {
		i--
	}
	if i < 0 {
		return nil, trailers
	}
	start := i
	for start >= 0 {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			break
		}
		if !isTrailerLine(trimmed) {
			return lines[:i+1], trailers
		}
		start--
	}
	trailerLines := lines[start+1 : i+1]
	for _, line := range trailerLines {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		trailers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	bodyEnd := start
	for bodyEnd >= 0 && strings.TrimSpace(lines[bodyEnd]) == "" {
		bodyEnd--
	}
	if bodyEnd < 0 {
		return nil, trailers
	}
	return lines[:bodyEnd+1], trailers
}

func resolveRevision(repo *git.Repository, ref string) (plumbing.Hash, error) {
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve %s: %w", ref, err)
	}
	return *hash, nil
}

func parseTrailers(raw string) map[string]string {
	trailers := map[string]string{}
	if raw == "" {
		return trailers
	}
	for _, line := range strings.Split(raw, "\x1e") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		trailers[key] = value
	}
	return trailers
}

func cleanBody(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			filtered = append(filtered, "")
			continue
		}
		if isTrailerLine(trimmed) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func isTrailerLine(line string) bool {
	if !strings.Contains(line, ":") {
		return false
	}
	prefix, _, _ := strings.Cut(line, ":")
	if prefix == "" {
		return false
	}
	for _, r := range prefix {
		if !(r == '-' || r == '_' || r == ' ' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')) {
			return false
		}
	}
	return true
}
