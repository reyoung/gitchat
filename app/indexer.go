package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/reyoung/gitchat/gitrepo"
	"github.com/reyoung/gitchat/model"
	"github.com/reyoung/gitchat/store"
)

type Indexer struct {
	Repo  *gitrepo.Repo
	Store *store.Store
}

func (i *Indexer) Run(ctx context.Context) error {
	if err := i.Store.Migrate(ctx); err != nil {
		return err
	}
	refs, err := i.Repo.ListRefs(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		cached, ok, err := i.Store.GetRefHead(ctx, ref.Name)
		if err != nil {
			return err
		}
		if ok && cached == ref.HeadHash {
			continue
		}
		if err := i.indexRef(ctx, ref); err != nil {
			return fmt.Errorf("index %s: %w", ref.Name, err)
		}
		if err := i.Store.UpdateRefHead(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) indexRef(ctx context.Context, ref model.RefState) error {
	sourceRef, canonicalRef, ok := normalizeTrackedRef(ref.Name)
	if !ok {
		return nil
	}
	switch {
	case strings.HasPrefix(canonicalRef, "users/"):
		return i.indexUserBranch(ctx, sourceRef, canonicalRef)
	case strings.HasPrefix(canonicalRef, "channels/"):
		return i.indexChannelBranch(ctx, sourceRef, canonicalRef)
	case strings.HasPrefix(canonicalRef, "experiments/"):
		return i.indexExperimentBranch(ctx, sourceRef, canonicalRef)
	default:
		return nil
	}
}

func (i *Indexer) indexUserBranch(ctx context.Context, sourceRef, branch string) error {
	userID := strings.TrimPrefix(branch, "users/")
	commits, err := i.Repo.ListCommits(ctx, sourceRef)
	if err != nil {
		return err
	}
	user := model.User{ID: userID, Branch: branch}
	messages := make([]model.Message, 0, len(commits))
	for _, commit := range commits {
		if avatarURL := strings.TrimSpace(commit.Trailers["User-Avatar-URL"]); avatarURL != "" {
			user.AvatarURL = avatarURL
		}
		channelID := commit.Trailers["Channel"]
		if channelID == "" {
			continue
		}
		messages = append(messages, model.Message{
			CommitHash:    commit.Hash,
			UserID:        userID,
			Branch:        branch,
			ChannelID:     channelID,
			Subject:       commit.Subject,
			Body:          commit.Body,
			ReplyTo:       commit.Trailers["Reply-To"],
			EditOf:        commit.Trailers["Edit-Of"],
			Follows:       splitCSV(commit.Trailers["Follows"]),
			ExperimentID:  commit.Trailers["Experiment"],
			ExperimentSHA: commit.Trailers["Experiment-SHA"],
			CreatedAt:     commit.CommitTime,
		})
	}
	if err := i.Store.ReplaceUserBranch(ctx, user); err != nil {
		return err
	}
	return i.Store.ReplaceUserMessages(ctx, branch, messages)
}

func (i *Indexer) indexChannelBranch(ctx context.Context, sourceRef, branch string) error {
	channelID := strings.TrimPrefix(branch, "channels/")
	commits, err := i.Repo.ListCommits(ctx, sourceRef)
	if err != nil {
		return err
	}
	channel := model.Channel{ID: channelID, Branch: branch}
	events := make([]model.ChannelEvent, 0, len(commits))
	for idx, commit := range commits {
		eventChannelID := firstNonEmpty(commit.Trailers["Channel-Id"], channelID)
		eventType := commit.Trailers["Event-Type"]
		actor := commit.Trailers["Actor"]
		if idx == 0 {
			channel.Creator = firstNonEmpty(commit.Trailers["Channel-Creator"], actor)
			channel.Title = commit.Trailers["Channel-Title"]
			channel.IsPublic = strings.EqualFold(firstNonEmpty(commit.Trailers["Channel-Visibility"], "private"), "public")
		}
		if eventType == "" {
			continue
		}
		events = append(events, model.ChannelEvent{
			CommitHash: commit.Hash,
			Branch:     branch,
			ChannelID:  eventChannelID,
			EventType:  eventType,
			Actor:      actor,
			Member:     commit.Trailers["Member"],
			CreatedAt:  commit.CommitTime,
		})
	}
	if err := i.Store.ReplaceChannelBranch(ctx, channel); err != nil {
		return err
	}
	return i.Store.ReplaceChannelEvents(ctx, branch, events)
}

func (i *Indexer) indexExperimentBranch(ctx context.Context, sourceRef, branch string) error {
	experimentID := strings.TrimPrefix(branch, "experiments/")
	commits, err := i.Repo.ListCommits(ctx, sourceRef)
	if err != nil {
		return err
	}
	experiment := model.Experiment{ID: experimentID, Branch: branch}
	events := make([]model.ExperimentEvent, 0, len(commits))
	for idx, commit := range commits {
		eventExperimentID := firstNonEmpty(commit.Trailers["Experiment-Id"], experimentID)
		eventType := commit.Trailers["Event-Type"]
		actor := commit.Trailers["Actor"]
		if idx == 0 {
			experiment.Creator = actor
			experiment.Title = commit.Subject
		}
		if eventType == "" {
			continue
		}
		events = append(events, model.ExperimentEvent{
			CommitHash:   commit.Hash,
			Branch:       branch,
			ExperimentID: eventExperimentID,
			EventType:    eventType,
			Actor:        actor,
			RetainedSHA:  commit.Trailers["Retained-SHA"],
			CreatedAt:    commit.CommitTime,
		})
	}
	if err := i.Store.ReplaceExperimentBranch(ctx, experiment); err != nil {
		return err
	}
	return i.Store.ReplaceExperimentEvents(ctx, branch, events)
}

func normalizeTrackedRef(name string) (sourceRef string, canonicalRef string, ok bool) {
	name = strings.TrimSpace(name)
	switch {
	case strings.HasPrefix(name, "users/"),
		strings.HasPrefix(name, "channels/"),
		strings.HasPrefix(name, "experiments/"):
		return name, name, true
	case strings.HasPrefix(name, "origin/users/"),
		strings.HasPrefix(name, "origin/channels/"),
		strings.HasPrefix(name, "origin/experiments/"):
		return name, strings.TrimPrefix(name, "origin/"), true
	default:
		return "", "", false
	}
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
