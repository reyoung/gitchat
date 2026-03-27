package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"os/exec"
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
	return s.CreateUserProfile(ctx, userID, keyPath, "")
}

func (s *Service) CreateUserProfile(ctx context.Context, userID, keyPath, avatarURL string) error {
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
			"avatar_url": strings.TrimSpace(avatarURL),
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
		if _, err := s.Repo.CommitFilesToBranch(ctx, "main", gitrepo.BuildCommitMessage("register user "+userID, "", map[string]string{
			"User-Id":         userID,
			"User-Avatar-URL": strings.TrimSpace(avatarURL),
		}), files); err != nil {
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

func (s *Service) UpdateUserProfile(ctx context.Context, userID, avatarURL string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	avatarURL = strings.TrimSpace(avatarURL)
	if err := s.Sync(ctx); err != nil {
		return err
	}
	if err := s.ensureUserBranch(ctx, userID); err != nil {
		return err
	}
	return s.Repo.WithSavedBranch(ctx, func() error {
		branch := "users/" + userID
		if err := s.Repo.SwitchBranch(ctx, branch); err != nil {
			return err
		}
		msg := gitrepo.BuildCommitMessage(
			fmt.Sprintf("update profile %s", userID),
			"",
			map[string]string{
				"Event-Type":      "update-user-profile",
				"User-Id":         userID,
				"User-Avatar-URL": avatarURL,
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

func (s *Service) ListUsers(ctx context.Context) ([]model.User, error) {
	return s.Store.ListUsers(ctx)
}

func (s *Service) ListExperiments(ctx context.Context) ([]model.Experiment, error) {
	return s.Store.ListExperiments(ctx)
}

func (s *Service) ListMessagesByChannel(ctx context.Context, channelID string) ([]model.Message, error) {
	return s.Store.ListMessagesByChannel(ctx, channelID)
}

type UploadedAttachment struct {
	CommitHash string
	Path       string
	Markdown   string
}

func (s *Service) UploadImageAttachment(ctx context.Context, userID, channelID, sourcePath string) (UploadedAttachment, error) {
	userID = strings.TrimSpace(userID)
	channelID = strings.TrimSpace(channelID)
	sourcePath = strings.TrimSpace(sourcePath)
	if userID == "" || channelID == "" || sourcePath == "" {
		return UploadedAttachment{}, fmt.Errorf("user, channel, and source path are required")
	}
	if err := s.Sync(ctx); err != nil {
		return UploadedAttachment{}, err
	}
	if err := s.ensureUserBranch(ctx, userID); err != nil {
		return UploadedAttachment{}, err
	}
	destPath := filepath.ToSlash(filepath.Join("attachments", channelID, fmt.Sprintf("%d-%s", s.Now().UTC().UnixNano(), filepath.Base(sourcePath))))
	uploaded, err := s.uploadLFSAssetToUserBranch(ctx, userID, destPath, sourcePath, "attachments/**", fmt.Sprintf("upload attachment %s", filepath.Base(destPath)))
	if err != nil {
		return UploadedAttachment{}, err
	}
	uploaded.Markdown = fmt.Sprintf("![%s](%s)", filepath.Base(destPath), buildGitAssetURI(uploaded.CommitHash, uploaded.Path))
	if err := s.Sync(ctx); err != nil {
		return UploadedAttachment{}, err
	}
	return uploaded, nil
}

func (s *Service) UploadImageDataURL(ctx context.Context, userID, channelID, filename, dataURL string) (UploadedAttachment, error) {
	userID = strings.TrimSpace(userID)
	channelID = strings.TrimSpace(channelID)
	filename = strings.TrimSpace(filename)
	dataURL = strings.TrimSpace(dataURL)
	if userID == "" || channelID == "" || dataURL == "" {
		return UploadedAttachment{}, fmt.Errorf("user, channel, and image data are required")
	}
	header, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(header, "data:") {
		return UploadedAttachment{}, fmt.Errorf("invalid data url")
	}
	if !strings.Contains(header, ";base64") {
		return UploadedAttachment{}, fmt.Errorf("image data url must be base64 encoded")
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return UploadedAttachment{}, fmt.Errorf("decode image data: %w", err)
	}
	if filename == "" {
		filename = "pasted-image"
	}
	if filepath.Ext(filename) == "" {
		mimeType := strings.TrimPrefix(strings.Split(strings.TrimPrefix(header, "data:"), ";")[0], " ")
		if exts, extErr := mime.ExtensionsByType(mimeType); extErr == nil && len(exts) > 0 {
			filename += exts[0]
		} else {
			filename += ".png"
		}
	}
	tmpFile, err := os.CreateTemp("", "gitchat-paste-*"+filepath.Ext(filename))
	if err != nil {
		return UploadedAttachment{}, err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(payload); err != nil {
		tmpFile.Close()
		return UploadedAttachment{}, err
	}
	if err := tmpFile.Close(); err != nil {
		return UploadedAttachment{}, err
	}
	return s.UploadImageAttachment(ctx, userID, channelID, tmpFile.Name())
}

func (s *Service) SetUserAvatarFromFile(ctx context.Context, userID, sourcePath string) (string, error) {
	userID = strings.TrimSpace(userID)
	sourcePath = strings.TrimSpace(sourcePath)
	if userID == "" || sourcePath == "" {
		return "", fmt.Errorf("user and source path are required")
	}
	if err := s.Sync(ctx); err != nil {
		return "", err
	}
	if err := s.ensureUserBranch(ctx, userID); err != nil {
		return "", err
	}
	destPath := filepath.ToSlash(filepath.Join("avatars", userID, fmt.Sprintf("%d-%s", s.Now().UTC().UnixNano(), filepath.Base(sourcePath))))
	uploaded, err := s.uploadLFSAssetToUserBranch(ctx, userID, destPath, sourcePath, "avatars/**", fmt.Sprintf("upload avatar %s", filepath.Base(destPath)))
	if err != nil {
		return "", err
	}
	avatarURL := buildGitAssetURI(uploaded.CommitHash, uploaded.Path)
	if err := s.UpdateUserProfile(ctx, userID, avatarURL); err != nil {
		return "", err
	}
	return avatarURL, nil
}

func (s *Service) uploadLFSAssetToUserBranch(ctx context.Context, userID, destPath, sourcePath, trackPattern, commitMessage string) (UploadedAttachment, error) {
	repoSpec := s.repoSpec()
	if repoSpec == "" {
		return UploadedAttachment{}, fmt.Errorf("repo spec is required")
	}
	tmpDir, err := os.MkdirTemp("", "gitchat-upload-*")
	if err != nil {
		return UploadedAttachment{}, err
	}
	defer os.RemoveAll(tmpDir)

	if err := runGit(ctx, "", "clone", "--branch", "users/"+userID, "--single-branch", repoSpec, tmpDir); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "config", "user.name", userID); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "config", "user.email", fmt.Sprintf("%s@gitchat.local", userID)); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "lfs", "install", "--local"); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "lfs", "track", trackPattern); err != nil {
		return UploadedAttachment{}, err
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, filepath.Dir(destPath)), 0o755); err != nil {
		return UploadedAttachment{}, err
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return UploadedAttachment{}, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, destPath), data, 0o644); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "add", ".gitattributes", destPath); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "commit", "-m", commitMessage); err != nil {
		return UploadedAttachment{}, err
	}
	if err := runGit(ctx, tmpDir, "push", "origin", "HEAD:users/"+userID); err != nil {
		return UploadedAttachment{}, err
	}
	commitHash, err := runGitOutput(ctx, tmpDir, "rev-parse", "HEAD")
	if err != nil {
		return UploadedAttachment{}, err
	}
	return UploadedAttachment{
		CommitHash: strings.TrimSpace(commitHash),
		Path:       destPath,
	}, nil
}

func (s *Service) LoadAttachmentDataURL(ctx context.Context, commitHash, relPath string) (string, error) {
	commitHash = strings.TrimSpace(commitHash)
	relPath = strings.TrimSpace(relPath)
	if commitHash == "" || relPath == "" {
		return "", fmt.Errorf("commit hash and path are required")
	}
	repoSpec := s.repoSpec()
	if repoSpec == "" {
		return "", fmt.Errorf("repo spec is required")
	}
	tmpDir, err := os.MkdirTemp("", "gitchat-attachment-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	if err := runGit(ctx, "", "clone", repoSpec, tmpDir); err != nil {
		return "", err
	}
	if err := runGit(ctx, tmpDir, "checkout", commitHash); err != nil {
		return "", err
	}
	_ = runGit(ctx, tmpDir, "lfs", "install", "--local")
	_ = runGit(ctx, tmpDir, "lfs", "pull")

	data, err := os.ReadFile(filepath.Join(tmpDir, filepath.FromSlash(relPath)))
	if err != nil {
		return "", err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(relPath)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func (s *Service) repoSpec() string {
	if strings.TrimSpace(s.Repo.RemoteURL) != "" {
		return strings.TrimSpace(s.Repo.RemoteURL)
	}
	return strings.TrimSpace(s.Repo.Dir)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := runGitOutput(ctx, dir, args...)
	return err
}

func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func buildGitAssetURI(commitHash, path string) string {
	return fmt.Sprintf("gitchat-attachment://%s?path=%s", strings.TrimSpace(commitHash), url.QueryEscape(path))
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
