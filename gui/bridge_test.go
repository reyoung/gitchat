package gui

import (
	"context"
	"testing"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/model"
)

type fakeService struct {
	sendInput    app.SendMessageInput
	sendCalled   bool
	channels     []model.Channel
	experiments  []model.Experiment
	messages     []model.Message
	defaultError error
}

func (f *fakeService) Sync(context.Context) error { return f.defaultError }
func (f *fakeService) CreateUser(context.Context, string, string) error {
	return f.defaultError
}
func (f *fakeService) CreateChannel(context.Context, string, string, string) error {
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
func (f *fakeService) ListChannels(context.Context) ([]model.Channel, error) {
	return f.channels, f.defaultError
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
			{ID: "research", Title: "Research", Creator: "alice"},
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
