package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/model"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type serviceAPI interface {
	Sync(context.Context) error
	CreateUser(context.Context, string, string) error
	UpdateUserProfile(context.Context, string, string) error
	CreateChannel(context.Context, string, string, string) error
	AddChannelMember(context.Context, string, string, string) error
	CreateExperiment(context.Context, string, string, string, string, string) error
	RetainExperimentAttempt(context.Context, string, string) error
	SendMessage(context.Context, app.SendMessageInput) error
	UploadImageAttachment(context.Context, string, string, string) (app.UploadedAttachment, error)
	LoadAttachmentDataURL(context.Context, string, string) (string, error)
	ListChannels(context.Context) ([]model.Channel, error)
	ListUsers(context.Context) ([]model.User, error)
	ListExperiments(context.Context) ([]model.Experiment, error)
	ListMessagesByChannel(context.Context, string) ([]model.Message, error)
}

type Bridge struct {
	ctx      context.Context
	svc      serviceAPI
	defaults Defaults
}

type AppState struct {
	CurrentUser          string           `json:"currentUser"`
	CurrentUserAvatarURL string           `json:"currentUserAvatarURL"`
	SelectedChannel      string           `json:"selectedChannel"`
	SelectedChannelTitle string           `json:"selectedChannelTitle"`
	Channels             []ChannelView    `json:"channels"`
	Experiments          []ExperimentView `json:"experiments"`
	Messages             []MessageView    `json:"messages"`
}

type ChannelView struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Creator string `json:"creator"`
}

type ExperimentView struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Creator string `json:"creator"`
}

type MessageView struct {
	CommitHash    string `json:"commitHash"`
	ShortHash     string `json:"shortHash"`
	UserID        string `json:"userID"`
	AvatarURL     string `json:"avatarURL"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	ReplyTo       string `json:"replyTo"`
	EditOf        string `json:"editOf"`
	ExperimentID  string `json:"experimentID"`
	ExperimentSHA string `json:"experimentSHA"`
	CreatedAt     string `json:"createdAt"`
}

type SendMessageRequest struct {
	UserID        string `json:"userID"`
	ChannelID     string `json:"channelID"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	ReplyTo       string `json:"replyTo"`
	EditOf        string `json:"editOf"`
	ExperimentID  string `json:"experimentID"`
	ExperimentSHA string `json:"experimentSHA"`
}

type CreateUserRequest struct {
	UserID string `json:"userID"`
}

type UpdateUserProfileRequest struct {
	UserID    string `json:"userID"`
	AvatarURL string `json:"avatarURL"`
}

type InsertImageRequest struct {
	UserID    string `json:"userID"`
	ChannelID string `json:"channelID"`
}

type ResolveAttachmentRequest struct {
	CommitHash string `json:"commitHash"`
	Path       string `json:"path"`
}

type CreateChannelRequest struct {
	ChannelID string `json:"channelID"`
	Creator   string `json:"creator"`
	Title     string `json:"title"`
}

type AddMemberRequest struct {
	ChannelID string `json:"channelID"`
	Actor     string `json:"actor"`
	Member    string `json:"member"`
}

type CreateExperimentRequest struct {
	ExperimentID string `json:"experimentID"`
	Actor        string `json:"actor"`
	Title        string `json:"title"`
	BaseRef      string `json:"baseRef"`
	ConfigJSON   string `json:"configJSON"`
}

type RetainAttemptRequest struct {
	ExperimentID string `json:"experimentID"`
	Ref          string `json:"ref"`
}

func NewBridge(svc serviceAPI, defaults Defaults) *Bridge {
	return &Bridge{svc: svc, defaults: defaults}
}

func (b *Bridge) startup(ctx context.Context) {
	b.ctx = ctx
}

func (b *Bridge) GetState(selectedChannel string) (AppState, error) {
	if err := b.svc.Sync(context.Background()); err != nil {
		return AppState{}, err
	}
	return b.loadState(selectedChannel)
}

func (b *Bridge) SendMessage(req SendMessageRequest) (AppState, error) {
	if strings.TrimSpace(req.UserID) == "" {
		req.UserID = b.defaults.UserName
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return AppState{}, fmt.Errorf("channel is required")
	}
	subject := strings.TrimSpace(req.Subject)
	body := strings.TrimSpace(req.Body)
	if subject == "" && body == "" {
		return AppState{}, fmt.Errorf("message body is required")
	}
	if subject == "" {
		subject = firstLine(body)
	}
	if err := b.svc.SendMessage(context.Background(), app.SendMessageInput{
		UserID:        req.UserID,
		ChannelID:     req.ChannelID,
		Subject:       subject,
		Body:          body,
		ReplyTo:       strings.TrimSpace(req.ReplyTo),
		EditOf:        strings.TrimSpace(req.EditOf),
		ExperimentID:  strings.TrimSpace(req.ExperimentID),
		ExperimentSHA: strings.TrimSpace(req.ExperimentSHA),
	}); err != nil {
		return AppState{}, err
	}
	return b.loadState(req.ChannelID)
}

func (b *Bridge) CreateUser(req CreateUserRequest) (AppState, error) {
	if err := b.svc.CreateUser(context.Background(), strings.TrimSpace(req.UserID), ""); err != nil {
		return AppState{}, err
	}
	b.defaults.UserName = strings.TrimSpace(req.UserID)
	return b.loadState("")
}

func (b *Bridge) UpdateUserProfile(req UpdateUserProfileRequest) (AppState, error) {
	userID := firstNonEmpty(req.UserID, b.defaults.UserName)
	if err := b.svc.UpdateUserProfile(context.Background(), userID, strings.TrimSpace(req.AvatarURL)); err != nil {
		return AppState{}, err
	}
	return b.loadState("")
}

func (b *Bridge) CreateChannel(req CreateChannelRequest) (AppState, error) {
	creator := firstNonEmpty(req.Creator, b.defaults.UserName)
	if err := b.svc.CreateChannel(context.Background(), strings.TrimSpace(req.ChannelID), creator, strings.TrimSpace(req.Title)); err != nil {
		return AppState{}, err
	}
	return b.loadState(strings.TrimSpace(req.ChannelID))
}

func (b *Bridge) AddChannelMember(req AddMemberRequest) (AppState, error) {
	actor := firstNonEmpty(req.Actor, b.defaults.UserName)
	if err := b.svc.AddChannelMember(context.Background(), strings.TrimSpace(req.ChannelID), actor, strings.TrimSpace(req.Member)); err != nil {
		return AppState{}, err
	}
	return b.loadState(strings.TrimSpace(req.ChannelID))
}

func (b *Bridge) CreateExperiment(req CreateExperimentRequest) (AppState, error) {
	actor := firstNonEmpty(req.Actor, b.defaults.UserName)
	if err := b.svc.CreateExperiment(
		context.Background(),
		strings.TrimSpace(req.ExperimentID),
		actor,
		strings.TrimSpace(req.Title),
		firstNonEmpty(req.BaseRef, "HEAD"),
		firstNonEmpty(req.ConfigJSON, "{}"),
	); err != nil {
		return AppState{}, err
	}
	return b.loadState("")
}

func (b *Bridge) InsertImage(req InsertImageRequest) (string, error) {
	userID := firstNonEmpty(req.UserID, b.defaults.UserName)
	channelID := strings.TrimSpace(req.ChannelID)
	if userID == "" {
		return "", fmt.Errorf("user is required")
	}
	if channelID == "" {
		return "", fmt.Errorf("channel is required")
	}
	path, err := runtime.OpenFileDialog(b.ctx, runtime.OpenDialogOptions{
		Title: "Insert image",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Images",
				Pattern:     "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.svg",
			},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	attachment, err := b.svc.UploadImageAttachment(context.Background(), userID, channelID, path)
	if err != nil {
		return "", err
	}
	return attachment.Markdown, nil
}

func (b *Bridge) ResolveAttachment(req ResolveAttachmentRequest) (string, error) {
	return b.svc.LoadAttachmentDataURL(context.Background(), strings.TrimSpace(req.CommitHash), strings.TrimSpace(req.Path))
}

func (b *Bridge) RetainExperiment(req RetainAttemptRequest) (AppState, error) {
	if err := b.svc.RetainExperimentAttempt(context.Background(), strings.TrimSpace(req.ExperimentID), strings.TrimSpace(req.Ref)); err != nil {
		return AppState{}, err
	}
	return b.loadState("")
}

func (b *Bridge) loadState(selectedChannel string) (AppState, error) {
	channels, err := b.svc.ListChannels(context.Background())
	if err != nil {
		return AppState{}, err
	}
	users, err := b.svc.ListUsers(context.Background())
	if err != nil {
		return AppState{}, err
	}
	experiments, err := b.svc.ListExperiments(context.Background())
	if err != nil {
		return AppState{}, err
	}
	selectedChannel = resolveSelectedChannel(selectedChannel, channels)
	state := AppState{
		CurrentUser:     b.defaults.UserName,
		SelectedChannel: selectedChannel,
		Channels:        make([]ChannelView, 0, len(channels)),
		Experiments:     make([]ExperimentView, 0, len(experiments)),
	}
	userAvatarByID := map[string]string{}
	for _, user := range users {
		userAvatarByID[user.ID] = user.AvatarURL
		if user.ID == b.defaults.UserName {
			state.CurrentUserAvatarURL = user.AvatarURL
		}
	}
	for _, channel := range channels {
		title := strings.TrimSpace(channel.Title)
		if title == "" {
			title = channel.ID
		}
		state.Channels = append(state.Channels, ChannelView{
			ID:      channel.ID,
			Title:   title,
			Creator: channel.Creator,
		})
		if channel.ID == selectedChannel {
			state.SelectedChannelTitle = title
		}
	}
	for _, experiment := range experiments {
		title := strings.TrimSpace(experiment.Title)
		if title == "" {
			title = experiment.ID
		}
		state.Experiments = append(state.Experiments, ExperimentView{
			ID:      experiment.ID,
			Title:   title,
			Creator: experiment.Creator,
		})
	}
	if selectedChannel == "" {
		return state, nil
	}
	messages, err := b.svc.ListMessagesByChannel(context.Background(), selectedChannel)
	if err != nil {
		return AppState{}, err
	}
	state.Messages = make([]MessageView, 0, len(messages))
	for _, message := range messages {
		state.Messages = append(state.Messages, MessageView{
			CommitHash:    message.CommitHash,
			ShortHash:     shortSHA(message.CommitHash),
			UserID:        message.UserID,
			AvatarURL:     userAvatarByID[message.UserID],
			Subject:       message.Subject,
			Body:          message.Body,
			ReplyTo:       message.ReplyTo,
			EditOf:        message.EditOf,
			ExperimentID:  message.ExperimentID,
			ExperimentSHA: shortSHA(message.ExperimentSHA),
			CreatedAt:     formatTimestamp(message.CreatedAt),
		})
	}
	return state, nil
}

func resolveSelectedChannel(selected string, channels []model.Channel) string {
	selected = strings.TrimSpace(selected)
	if selected != "" {
		for _, channel := range channels {
			if channel.ID == selected {
				return selected
			}
		}
	}
	if len(channels) == 0 {
		return ""
	}
	return channels[0].ID
}

func shortSHA(value string) string {
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04")
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		value = value[:idx]
	}
	if len(value) > 72 {
		return value[:72]
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
