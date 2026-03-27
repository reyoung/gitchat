package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/reyoung/gitchat/gitrepo"
	"github.com/reyoung/gitchat/model"
	"github.com/reyoung/gitchat/store"
)

type Service struct {
	Repo       *gitrepo.Repo
	Store      *store.Store
	Now        func() time.Time
	RemoteName string
}

func NewService(repo *gitrepo.Repo, store *store.Store) *Service {
	return &Service{
		Repo:       repo,
		Store:      store,
		Now:        time.Now,
		RemoteName: "",
	}
}

func (s *Service) Init(ctx context.Context) error {
	return s.Store.Migrate(ctx)
}

func (s *Service) Sync(ctx context.Context) error {
	if s.RemoteName != "" {
		if err := s.Repo.Fetch(ctx, s.RemoteName); err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return err
		}
	}
	return (&Indexer{Repo: s.Repo, Store: s.Store}).Run(ctx)
}

func (s *Service) CreateUser(ctx context.Context, userID, keyPath string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		if err := s.ensureMainBranch(ctx); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(map[string]any{
			"id":         userID,
			"created_at": s.Now().UTC().Format(time.RFC3339),
		}, "", "  ")
		if err != nil {
			return err
		}
		files := map[string][]byte{
			filepath.ToSlash(filepath.Join("users", userID+".json")): append(payload, '\n'),
		}
		if keyPath != "" {
			keyData, err := os.ReadFile(keyPath)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(filepath.Join("keys", userID+".pub"))] = keyData
		}
		if _, err := s.Repo.CommitFilesToBranch(ctx, "main", gitrepo.BuildCommitMessage("register user "+userID, "", nil), files); err != nil {
			return err
		}
		mainHead, err := s.Repo.RevParse(ctx, "main")
		if err != nil {
			return err
		}
		if err := s.Repo.EnsureBranch(ctx, "users/"+userID, mainHead); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, "main", "users/"+userID); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

func (s *Service) CreateChannel(ctx context.Context, channelID, creator, title string) error {
	channelID = strings.TrimSpace(channelID)
	creator = strings.TrimSpace(creator)
	if channelID == "" || creator == "" {
		return fmt.Errorf("channel id and creator are required")
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		if err := s.ensureMainBranch(ctx); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(map[string]any{
			"id":         channelID,
			"creator":    creator,
			"title":      title,
			"created_at": s.Now().UTC().Format(time.RFC3339),
		}, "", "  ")
		if err != nil {
			return err
		}
		if _, err := s.Repo.CommitFilesToBranch(ctx, "main", gitrepo.BuildCommitMessage("create channel "+channelID, "", nil), map[string][]byte{
			filepath.ToSlash(filepath.Join("channels", channelID+".json")): append(payload, '\n'),
		}); err != nil {
			return err
		}
		mainHead, err := s.Repo.RevParse(ctx, "main")
		if err != nil {
			return err
		}
		if err := s.Repo.EnsureBranch(ctx, "channels/"+channelID, mainHead); err != nil {
			return err
		}
		if err := s.Repo.SwitchBranch(ctx, "channels/"+channelID); err != nil {
			return err
		}
		msg := gitrepo.BuildCommitMessage(
			"create channel "+channelID,
			"",
			map[string]string{
				"Channel-Id":      channelID,
				"Channel-Creator": creator,
				"Channel-Title":   title,
				"Event-Type":      "create",
				"Actor":           creator,
				"Member":          creator,
			},
		)
		if err := s.Repo.Commit(ctx, msg, true); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, "main", "channels/"+channelID); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

func (s *Service) AddChannelMember(ctx context.Context, channelID, actor, member string) error {
	channelID = strings.TrimSpace(channelID)
	actor = strings.TrimSpace(actor)
	member = strings.TrimSpace(member)
	if channelID == "" || actor == "" || member == "" {
		return fmt.Errorf("channel id, actor, and member are required")
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		branch := "channels/" + channelID
		if err := s.Repo.SwitchBranch(ctx, branch); err != nil {
			return err
		}
		msg := gitrepo.BuildCommitMessage(
			fmt.Sprintf("add %s to %s", member, channelID),
			"",
			map[string]string{
				"Channel-Id": channelID,
				"Event-Type": "add-member",
				"Actor":      actor,
				"Member":     member,
			},
		)
		if err := s.Repo.Commit(ctx, msg, true); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, branch); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

func (s *Service) CreateExperiment(ctx context.Context, experimentID, actor, title, baseRef, configJSON string) error {
	experimentID = strings.TrimSpace(experimentID)
	actor = strings.TrimSpace(actor)
	if experimentID == "" || actor == "" {
		return fmt.Errorf("experiment id and actor are required")
	}
	if strings.TrimSpace(baseRef) == "" {
		baseRef = "HEAD"
	}
	if strings.TrimSpace(configJSON) == "" {
		configJSON = "{}"
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		if err := s.ensureMainBranch(ctx); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(map[string]any{
			"id":         experimentID,
			"creator":    actor,
			"title":      title,
			"created_at": s.Now().UTC().Format(time.RFC3339),
			"base_ref":   baseRef,
		}, "", "  ")
		if err != nil {
			return err
		}
		if err := s.Repo.WriteFile(filepath.ToSlash(filepath.Join("experiments", experimentID+".json")), append(payload, '\n')); err != nil {
			return err
		}
		if err := s.Repo.AddAll(ctx); err != nil {
			return err
		}
		if err := s.Repo.Commit(ctx, gitrepo.BuildCommitMessage("register experiment "+experimentID, "", nil), false); err != nil {
			return err
		}
		if s.Repo.BranchExists(ctx, "experiments/"+experimentID) {
			return fmt.Errorf("experiment branch already exists: %s", experimentID)
		}
		if err := s.Repo.SwitchNewBranch(ctx, "experiments/"+experimentID, baseRef); err != nil {
			return err
		}
		if err := s.Repo.WriteFile(filepath.ToSlash(filepath.Join("experiments", experimentID, "config.json")), []byte(strings.TrimSpace(configJSON)+"\n")); err != nil {
			return err
		}
		if err := s.Repo.AddAll(ctx); err != nil {
			return err
		}
		msg := gitrepo.BuildCommitMessage(
			firstNonEmpty(title, "start experiment "+experimentID),
			"",
			map[string]string{
				"Experiment-Id": experimentID,
				"Event-Type":    "create-experiment",
				"Actor":         actor,
				"Config-Path":   filepath.ToSlash(filepath.Join("experiments", experimentID, "config.json")),
			},
		)
		if err := s.Repo.Commit(ctx, msg, false); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, "main", "experiments/"+experimentID); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

func (s *Service) RetainExperimentAttempt(ctx context.Context, experimentID, retainedRef string) error {
	experimentID = strings.TrimSpace(experimentID)
	retainedRef = strings.TrimSpace(retainedRef)
	if experimentID == "" || retainedRef == "" {
		return fmt.Errorf("experiment id and retained ref are required")
	}
	sha, err := s.Repo.RevParse(ctx, retainedRef)
	if err != nil {
		return err
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		branch := "experiments/" + experimentID
		if err := s.Repo.SwitchBranch(ctx, branch); err != nil {
			return err
		}
		msg := gitrepo.BuildCommitMessage(
			fmt.Sprintf("retain attempt %s for %s", shortSHA(sha), experimentID),
			"",
			map[string]string{
				"Experiment-Id": experimentID,
				"Event-Type":    "retain-attempt",
				"Retained-SHA":  sha,
			},
		)
		if err := s.Repo.MergeOurs(ctx, sha, msg); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, branch); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

type SendMessageInput struct {
	UserID        string
	ChannelID     string
	Subject       string
	Body          string
	ReplyTo       string
	EditOf        string
	Follows       []string
	ExperimentID  string
	ExperimentSHA string
	Attachments   []string
}

func (s *Service) SendMessage(ctx context.Context, in SendMessageInput) error {
	if strings.TrimSpace(in.UserID) == "" || strings.TrimSpace(in.ChannelID) == "" {
		return fmt.Errorf("user and channel are required")
	}
	if err := s.Sync(ctx); err != nil {
		return err
	}
	if err := s.ensureUserBranch(ctx, in.UserID); err != nil {
		return err
	}
	if len(in.Follows) == 0 && strings.TrimSpace(in.EditOf) == "" {
		heads, err := s.ChannelHeads(ctx, in.ChannelID)
		if err != nil {
			return err
		}
		in.Follows = heads
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		branch := "users/" + in.UserID
		if err := s.ensureUserBranch(ctx, in.UserID); err != nil {
			return err
		}
		files := map[string][]byte{}
		for _, attachment := range in.Attachments {
			attachment = strings.TrimSpace(attachment)
			if attachment == "" {
				continue
			}
			dst := filepath.ToSlash(filepath.Join("attachments", in.ChannelID, fmt.Sprintf("%d-%s", s.Now().UTC().UnixNano(), filepath.Base(attachment))))
			data, err := os.ReadFile(attachment)
			if err != nil {
				return err
			}
			files[dst] = data
		}
		msg := gitrepo.BuildCommitMessage(
			firstNonEmpty(in.Subject, "(no subject)"),
			in.Body,
			map[string]string{
				"Channel":        in.ChannelID,
				"Follows":        strings.Join(in.Follows, ","),
				"Reply-To":       in.ReplyTo,
				"Edit-Of":        in.EditOf,
				"Experiment":     in.ExperimentID,
				"Experiment-SHA": in.ExperimentSHA,
				"Created-At":     s.Now().UTC().Format(time.RFC3339),
			},
		)
		messageHash, err := s.Repo.CreateOrphanCommit(ctx, msg, files)
		if err != nil {
			return err
		}
		anchorMessage := gitrepo.BuildCommitMessage(
			fmt.Sprintf("anchor message %s", shortSHA(messageHash)),
			"",
			nil,
		)
		if err := s.Repo.AnchorCommitToBranch(ctx, branch, messageHash, anchorMessage); err != nil {
			return err
		}
		if err := s.pushBranches(ctx, branch); err != nil {
			return err
		}
		return s.Sync(ctx)
	})
}

func (s *Service) ChannelHeads(ctx context.Context, channelID string) ([]string, error) {
	messages, err := s.Store.ListMessagesByChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	referenced := map[string]bool{}
	for _, message := range messages {
		if strings.TrimSpace(message.EditOf) != "" {
			continue
		}
		for _, followed := range message.Follows {
			referenced[followed] = true
		}
	}
	heads := make([]string, 0)
	for _, message := range messages {
		if strings.TrimSpace(message.EditOf) != "" {
			continue
		}
		if !referenced[message.CommitHash] {
			heads = append(heads, message.CommitHash)
		}
	}
	slices.Sort(heads)
	return heads, nil
}

func (s *Service) ListChannels(ctx context.Context) ([]model.Channel, error) {
	return s.Store.ListChannels(ctx)
}

func (s *Service) ListExperiments(ctx context.Context) ([]model.Experiment, error) {
	return s.Store.ListExperiments(ctx)
}

func (s *Service) ListMessagesByChannel(ctx context.Context, channelID string) ([]model.Message, error) {
	return s.Store.ListMessagesByChannel(ctx, channelID)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func (s *Service) pushBranches(ctx context.Context, branches ...string) error {
	if s.RemoteName == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		if err := s.Repo.Push(ctx, s.RemoteName, branch); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureMainBranch(ctx context.Context) error {
	if s.RemoteName != "" {
		if err := s.Repo.Fetch(ctx, s.RemoteName); err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return err
		}
	}
	if s.Repo.BranchExists(ctx, "main") {
		return s.Repo.SwitchBranch(ctx, "main")
	}
	if s.Repo.RefExists(ctx, "refs/remotes/origin/main") {
		return s.Repo.SwitchTrackBranch(ctx, "main", "refs/remotes/origin/main")
	}
	if s.Repo.HasCommit(ctx) {
		return s.Repo.SwitchNewBranch(ctx, "main", "HEAD")
	}
	return s.Repo.SwitchOrphanBranch(ctx, "main")
}

func (s *Service) ensureWritableBranch(ctx context.Context, branch string) error {
	if s.Repo.BranchExists(ctx, branch) {
		return nil
	}
	remoteRef := "refs/remotes/origin/" + branch
	if s.Repo.RefExists(ctx, remoteRef) {
		return s.Repo.EnsureBranch(ctx, branch, remoteRef)
	}
	return fmt.Errorf("branch %s does not exist", branch)
}

func (s *Service) ensureUserBranch(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	branch := "users/" + userID
	if s.Repo.BranchExists(ctx, branch) {
		return nil
	}
	if s.Repo.RefExists(ctx, "refs/remotes/origin/"+branch) {
		return s.Repo.EnsureBranch(ctx, branch, "refs/remotes/origin/"+branch)
	}
	if err := s.CreateUser(ctx, userID, ""); err != nil {
		return err
	}
	return s.ensureWritableBranch(ctx, branch)
}
