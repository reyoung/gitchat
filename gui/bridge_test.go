package gui

import (
	"context"
	"testing"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/model"
)

type fakeService struct {
	sendInput      app.SendMessageInput
	sendCalled     bool
	syncCalls      int
	forceSyncCalls int
	channels       []model.Channel
	experiments    []model.Experiment
	messages       []model.Message
	defaultError   error
}

func (f *fakeService) Sync(context.Context) error {
	f.syncCalls++
	return f.defaultError
}
func (f *fakeService) ForceSync(context.Context) error {
	f.forceSyncCalls++
	return f.defaultError
}
func (f *fakeService) CreateUser(context.Context, string, string) error {
	return f.defaultError
}
func (f *fakeService) UpdateUserProfile(context.Context, string, string) error { return f.defaultError }
func (f *fakeService) SetUserAvatarFromFile(context.Context, string, string) (string, error) {
	return "gitchat-attachment://abc?path=avatars%2Falice%2Favatar.png", f.defaultError
}
func (f *fakeService) CreateChannel(context.Context, string, string, string, bool) error {
	return f.defaultError
}
func (f *fakeService) AddChannelMember(context.Context, string, string, string) error {
	return f.defaultError
}
func (f *fakeService) CreateExperiment(context.Context, string, string, string, string, string) error {
	return f.defaultError
}
func (f *fakeService) RetainExperimentAttempt(context.Context, string, string) error {
	return f.defaultError
}
func (f *fakeService) SendMessage(_ context.Context, in app.SendMessageInput) error {
	f.sendCalled = true
	f.sendInput = in
	return f.defaultError
}
func (f *fakeService) UploadImageAttachment(context.Context, string, string, string) (app.UploadedAttachment, error) {
	return app.UploadedAttachment{Markdown: "![img](gitchat-attachment://abc?path=attachments%2Fresearch%2Fimage.png)"}, f.defaultError
}
func (f *fakeService) UploadImageDataURL(context.Context, string, string, string, string) (app.UploadedAttachment, error) {
	return app.UploadedAttachment{Markdown: "![paste](gitchat-attachment://abc?path=attachments%2Fresearch%2Fpaste.png)"}, f.defaultError
}
func (f *fakeService) LoadAttachmentDataURL(context.Context, string, string) (string, error) {
	return "data:image/png;base64,AAAA", f.defaultError
}
func (f *fakeService) ListChannels(context.Context) ([]model.Channel, error) {
	return f.channels, f.defaultError
}
func (f *fakeService) ListUsers(context.Context) ([]model.User, error) {
	return []model.User{{ID: "alice", Branch: "users/alice", AvatarURL: "https://example.com/a.png"}}, f.defaultError
}
func (f *fakeService) ListExperiments(context.Context) ([]model.Experiment, error) {
	return f.experiments, f.defaultError
}
func (f *fakeService) ListMessagesByChannel(_ context.Context, channelID string) ([]model.Message, error) {
	var result []model.Message
	for _, message := range f.messages {
		if message.ChannelID == channelID {
			result = append(result, message)
		}
	}
	return result, f.defaultError
}

func TestGetStateSelectsFirstChannelByDefault(t *testing.T) {
	svc := &fakeService{
		channels: []model.Channel{
			{ID: "research", Title: "Research", Creator: "alice", IsPublic: true},
			{ID: "release", Title: "Release", Creator: "alice"},
		},
		messages: []model.Message{
			{CommitHash: "abcdef123456", ChannelID: "research", UserID: "alice", Subject: "hello"},
		},
	}
	bridge := NewBridge(svc, Defaults{UserName: "alice"})

	state, err := bridge.GetState("")
	if err != nil {
		t.Fatalf("GetState returned error: %v", err)
	}
	if state.SelectedChannel != "research" {
		t.Fatalf("expected default selected channel research, got %q", state.SelectedChannel)
	}
	if len(state.Messages) != 1 || state.Messages[0].ShortHash != "abcdef1234" {
		t.Fatalf("unexpected messages: %+v", state.Messages)
	}
	if svc.forceSyncCalls != 1 {
		t.Fatalf("expected first GetState to force sync once, got %d", svc.forceSyncCalls)
	}
	if svc.syncCalls != 0 {
		t.Fatalf("expected first GetState not to use regular sync, got %d", svc.syncCalls)
	}
}

func TestGetStateUsesRegularSyncAfterInitialLoad(t *testing.T) {
	svc := &fakeService{
		channels: []model.Channel{
			{ID: "research", Title: "Research", Creator: "alice", IsPublic: true},
		},
	}
	bridge := NewBridge(svc, Defaults{UserName: "alice"})

	if _, err := bridge.GetState(""); err != nil {
		t.Fatalf("first GetState returned error: %v", err)
	}
	if _, err := bridge.GetState("research"); err != nil {
		t.Fatalf("second GetState returned error: %v", err)
	}
	if svc.forceSyncCalls != 1 {
		t.Fatalf("expected one force sync, got %d", svc.forceSyncCalls)
	}
	if svc.syncCalls != 1 {
		t.Fatalf("expected one regular sync after initial load, got %d", svc.syncCalls)
	}
}

func TestSendMessageUsesFirstLineAsFallbackSubject(t *testing.T) {
	svc := &fakeService{
		channels: []model.Channel{{ID: "research", Title: "Research", Creator: "alice"}},
	}
	bridge := NewBridge(svc, Defaults{UserName: "alice"})

	_, err := bridge.SendMessage(SendMessageRequest{
		ChannelID: "research",
		Body:      "hello world\nwith details",
	})
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if !svc.sendCalled {
		t.Fatal("expected SendMessage to be called")
	}
	if svc.sendInput.UserID != "alice" {
		t.Fatalf("expected default user alice, got %q", svc.sendInput.UserID)
	}
	if svc.sendInput.Subject != "hello world" {
		t.Fatalf("expected fallback subject hello world, got %q", svc.sendInput.Subject)
	}
}
