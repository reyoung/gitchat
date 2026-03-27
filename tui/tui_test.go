package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/model"
)

type fakeService struct {
	createUserCalled bool
	lastUser         string
	sendCalled       bool
	lastSend         app.SendMessageInput
}

func (f *fakeService) Sync(context.Context) error { return nil }
func (f *fakeService) CreateUser(_ context.Context, userID, _ string) error {
	f.createUserCalled = true
	f.lastUser = userID
	return nil
}
func (f *fakeService) CreateChannel(context.Context, string, string, string) error { return nil }
func (f *fakeService) AddChannelMember(context.Context, string, string, string) error {
	return nil
}
func (f *fakeService) CreateExperiment(context.Context, string, string, string, string, string) error {
	return nil
}
func (f *fakeService) RetainExperimentAttempt(context.Context, string, string) error { return nil }
func (f *fakeService) SendMessage(_ context.Context, in app.SendMessageInput) error {
	f.sendCalled = true
	f.lastSend = in
	return nil
}
func (f *fakeService) ListChannels(context.Context) ([]model.Channel, error)       { return nil, nil }
func (f *fakeService) ListExperiments(context.Context) ([]model.Experiment, error) { return nil, nil }
func (f *fakeService) ListMessagesByChannel(context.Context, string) ([]model.Message, error) {
	return nil, nil
}

func TestViewShowsChatLayout(t *testing.T) {
	m := newState(&fakeService{}, Defaults{UserName: "alice"})
	m.width = 100
	m.height = 30
	m.resize()
	m.setChannels([]model.Channel{{ID: "research", Creator: "alice"}})
	m.messages = []model.Message{{CommitHash: "abcdef1234567890", UserID: "alice", Subject: "hello"}}
	m.renderMessages()
	m.composer.SetValue("world")
	view := m.View()
	if !strings.Contains(view, "Channels") || !strings.Contains(view, "Messages") || !strings.Contains(view, "world") {
		t.Fatalf("unexpected view: %s", view)
	}
}

func TestTabSwitchCyclesFocus(t *testing.T) {
	m := newState(&fakeService{}, Defaults{UserName: "alice"})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(state)
	if updated.focus != focusChannels {
		t.Fatalf("expected focusChannels, got %v", updated.focus)
	}
}

func TestEnterOnComposerSendsMessage(t *testing.T) {
	svc := &fakeService{}
	m := newState(svc, Defaults{UserName: "alice"})
	m.setChannels([]model.Channel{{ID: "research", Creator: "alice"}})
	m.composer.SetValue("hello world")
	cmd, err := m.sendDraftCmd()
	if err != nil {
		t.Fatalf("unexpected send validation error: %v", err)
	}
	msg := cmd().(actionDoneMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if !svc.sendCalled {
		t.Fatal("expected SendMessage to be called")
	}
	if svc.lastSend.UserID != "alice" || svc.lastSend.ChannelID != "research" || svc.lastSend.Subject != "hello world" {
		t.Fatalf("unexpected message payload: %+v", svc.lastSend)
	}
}

func TestEnterOnComposerWithoutChannelShowsError(t *testing.T) {
	m := newState(&fakeService{}, Defaults{UserName: "alice"})
	m.composer.SetValue("hello world")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(state)
	if updated.err == nil || !strings.Contains(updated.err.Error(), "no channel selected") {
		t.Fatalf("expected no channel selected error, got %v", updated.err)
	}
}

func TestReplyShortcutSetsReplyTarget(t *testing.T) {
	m := newState(&fakeService{}, Defaults{UserName: "alice"})
	m.width = 100
	m.height = 30
	m.resize()
	m.messages = []model.Message{{CommitHash: "abcdef1234567890", UserID: "alice", Subject: "hello"}}
	m.renderMessages()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	updated := next.(state)
	if updated.replyTo != "abcdef1234567890" {
		t.Fatalf("expected reply target to be set, got %q", updated.replyTo)
	}
}

func TestOpenCreateUserForm(t *testing.T) {
	m := newState(&fakeService{}, Defaults{UserName: "alice"})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	updated := next.(state)
	if updated.form == nil || updated.form.action != "create-user" {
		t.Fatalf("expected create-user form, got %#v", updated.form)
	}
	if updated.form.fields[0].value != "alice" {
		t.Fatalf("expected default user alice, got %#v", updated.form.fields)
	}
}
