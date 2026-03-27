package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/model"
)

type serviceAPI interface {
	Sync(context.Context) error
	CreateUser(context.Context, string, string) error
	CreateChannel(context.Context, string, string, string) error
	AddChannelMember(context.Context, string, string, string) error
	CreateExperiment(context.Context, string, string, string, string, string) error
	RetainExperimentAttempt(context.Context, string, string) error
	SendMessage(context.Context, app.SendMessageInput) error
	ListChannels(context.Context) ([]model.Channel, error)
	ListExperiments(context.Context) ([]model.Experiment, error)
	ListMessagesByChannel(context.Context, string) ([]model.Message, error)
}

type Defaults struct {
	UserName string
}

type channelItem struct {
	channel model.Channel
}

func (i channelItem) Title() string       { return i.channel.ID }
func (i channelItem) Description() string { return i.channel.Creator }
func (i channelItem) FilterValue() string { return i.channel.ID + " " + i.channel.Creator + " " + i.channel.Title }

type field struct {
	label string
	value string
}

type form struct {
	title  string
	action string
	fields []field
	index  int
	input  textinput.Model
}

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	FocusLeft  key.Binding
	FocusRight key.Binding
	Send       key.Binding
	Reply      key.Binding
	Clear      key.Binding
	Refresh    key.Binding
	User       key.Binding
	Channel    key.Binding
	AddMember  key.Binding
	Experiment key.Binding
	Retain     key.Binding
	Quit       key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "move up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "move down")),
		FocusLeft:  key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab", "focus left")),
		FocusRight: key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab", "focus right")),
		Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send/open")),
		Reply:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		Clear:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear/cancel")),
		Refresh:    key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh")),
		User:       key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "user")),
		Channel:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "channel")),
		AddMember:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add member")),
		Experiment: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "experiment")),
		Retain:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "retain")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.FocusRight, k.Send, k.Reply, k.Refresh, k.Channel, k.AddMember, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.FocusLeft, k.FocusRight},
		{k.Send, k.Reply, k.Clear, k.Refresh},
		{k.User, k.Channel, k.AddMember, k.Experiment, k.Retain, k.Quit},
	}
}

type focusMode int

const (
	focusChannels focusMode = iota
	focusComposer
)

type state struct {
	svc         serviceAPI
	defaults    Defaults
	keys        keyMap
	help        help.Model
	focus       focusMode
	width       int
	height      int
	channelList list.Model
	viewport    viewport.Model
	composer    textinput.Model
	experiments []model.Experiment
	messages    []model.Message
	replyTo     string
	form        *form
	status      string
	err         error
}

type dataLoadedMsg struct {
	channels    []model.Channel
	experiments []model.Experiment
	messages    []model.Message
	err         error
}

type actionDoneMsg struct {
	status string
	err    error
}

type syncTickMsg struct{}

func Run(ctx context.Context, svc *app.Service, defaults Defaults) error {
	p := tea.NewProgram(newState(svc, defaults), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newState(svc serviceAPI, defaults Defaults) state {
	items := []list.Item{}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.SetSpacing(0)
	channelList := list.New(items, delegate, 28, 20)
	channelList.Title = "Channels"
	channelList.SetShowHelp(false)
	channelList.DisableQuitKeybindings()
	channelList.SetFilteringEnabled(false)
	channelList.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))

	vp := viewport.New(60, 20)
	vp.Style = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, true).BorderForeground(lipgloss.Color("240")).Padding(0, 1)

	composer := textinput.New()
	composer.Placeholder = "Type a message and press Enter"
	composer.Prompt = "> "
	composer.Focus()
	composer.CharLimit = 2048
	composer.Width = 60

	h := help.New()
	h.ShowAll = false

	return state{
		svc:         svc,
		defaults:    defaults,
		keys:        defaultKeyMap(),
		help:        h,
		focus:       focusComposer,
		channelList: channelList,
		viewport:    vp,
		composer:    composer,
	}
}

func (m state) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), syncTickCmd())
}

func (m state) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case dataLoadedMsg:
		m.err = msg.err
		m.experiments = msg.experiments
		if msg.err != nil {
			return m, nil
		}
		m.setChannels(msg.channels)
		m.messages = msg.messages
		m.renderMessages()
		return m, nil
	case actionDoneMsg:
		m.form = nil
		m.err = msg.err
		m.status = msg.status
		if msg.err == nil {
			m.replyTo = ""
			m.composer.SetValue("")
			return m, tea.Batch(m.loadCmd(), syncTickCmd())
		}
		return m, nil
	case syncTickMsg:
		return m, tea.Batch(m.loadCmd(), syncTickCmd())
	case tea.KeyMsg:
		if m.form != nil {
			return m.updateForm(msg)
		}
		return m.updateMain(msg)
	}

	var cmd tea.Cmd
	if m.focus == focusChannels {
		m.channelList, cmd = m.channelList.Update(msg)
	} else {
		m.composer, cmd = m.composer.Update(msg)
	}
	return m, cmd
}

func (m state) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadCmd()
	case key.Matches(msg, m.keys.FocusRight):
		if m.focus == focusChannels {
			m.focus = focusComposer
			m.composer.Focus()
		} else {
			m.focus = focusChannels
			m.composer.Blur()
		}
		return m, nil
	case key.Matches(msg, m.keys.FocusLeft):
		if m.focus == focusComposer {
			m.focus = focusChannels
			m.composer.Blur()
		} else {
			m.focus = focusComposer
			m.composer.Focus()
		}
		return m, nil
	case key.Matches(msg, m.keys.Clear):
		m.replyTo = ""
		m.status = ""
		m.err = nil
		return m, nil
	case key.Matches(msg, m.keys.Reply):
		if selected := m.selectedMessage(); selected != nil {
			m.replyTo = selected.CommitHash
			m.status = "replying to " + short(selected.CommitHash)
		}
		return m, nil
	case key.Matches(msg, m.keys.User):
		m.form = newForm("Create User", "create-user", []field{{label: "User ID", value: m.defaults.UserName}})
		return m, nil
	case key.Matches(msg, m.keys.Channel):
		m.form = newForm("Create Channel", "create-channel", []field{{label: "Channel ID"}, {label: "Title"}})
		return m, nil
	case key.Matches(msg, m.keys.AddMember):
		m.form = newForm("Add Channel Member", "add-member", []field{{label: "Channel ID", value: m.selectedChannelID()}, {label: "Member"}})
		return m, nil
	case key.Matches(msg, m.keys.Experiment):
		m.form = newForm("Create Experiment", "create-experiment", []field{{label: "Experiment ID"}, {label: "Title"}, {label: "Base Ref", value: "HEAD"}, {label: "Config JSON", value: "{}"}})
		return m, nil
	case key.Matches(msg, m.keys.Retain):
		m.form = newForm("Retain Attempt", "retain-attempt", []field{{label: "Experiment ID"}, {label: "Attempt SHA or Ref"}})
		return m, nil
	case key.Matches(msg, m.keys.Send):
		if m.focus == focusComposer {
			cmd, statusErr := m.sendDraftCmd()
			if statusErr != nil {
				m.status = ""
				m.err = statusErr
				return m, nil
			}
			return m, cmd
		}
		if m.focus == focusChannels {
			if cmd := m.reloadSelectedChannel(); cmd != nil {
				return m, cmd
			}
		}
	}

	if m.focus == focusChannels {
		prev := m.selectedChannelID()
		var cmd tea.Cmd
		m.channelList, cmd = m.channelList.Update(msg)
		if m.selectedChannelID() != prev {
			return m, m.reloadSelectedChannel()
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

func (m state) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.form = nil
		return m, nil
	case "tab", "down":
		m.form.save()
		if m.form.index < len(m.form.fields)-1 {
			m.form.index++
		}
		m.form.load()
		return m, nil
	case "shift+tab", "up":
		m.form.save()
		if m.form.index > 0 {
			m.form.index--
		}
		m.form.load()
		return m, nil
	case "enter":
		m.form.save()
		if m.form.index < len(m.form.fields)-1 {
			m.form.index++
			m.form.load()
			return m, nil
		}
		return m, m.submitFormCmd()
	}
	var cmd tea.Cmd
	m.form.input, cmd = m.form.input.Update(msg)
	return m, cmd
}

func (m state) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	if m.form != nil {
		return renderModal(m.width, m.height, m.renderShell(), renderForm(*m.form))
	}
	return m.renderShell()
}

func (m state) renderShell() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Render("GitChat")
	meta := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
		fmt.Sprintf(" user=%s  reply=%s  focus=%s", firstNonEmpty(m.defaults.UserName, "(unset)"), firstNonEmpty(short(m.replyTo), "-"), m.focusLabel()),
	)
	status := ""
	if m.status != "" {
		status = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("150")).Render(m.status)
	}
	if m.err != nil {
		status += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("error: "+m.err.Error())
	}

	left := m.channelList.View()
	right := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render("Messages"),
		m.viewport.View(),
		lipgloss.NewStyle().Bold(true).Render("Composer"),
		m.composer.View(),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(max(24, m.width/4)).Render(left),
		lipgloss.NewStyle().Width(max(30, m.width-max(24, m.width/4)-4)).Render(right),
	)
	helpView := m.help.View(m.keys)
	return lipgloss.JoinVertical(lipgloss.Left, header+meta, status, "", body, "", helpView)
}

func renderModal(width, height int, background, modal string) string {
	box := lipgloss.NewStyle().
		Width(min(72, max(36, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("69")).
		Padding(1, 2).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Render(modal)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderForm(f form) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(f.title))
	b.WriteString("\n\n")
	for i, field := range f.fields {
		label := field.label
		if i == f.index {
			label = "> " + label
		} else {
			label = "  " + label
		}
		b.WriteString(label + ": ")
		if i == f.index {
			b.WriteString(f.input.View())
		} else {
			b.WriteString(field.value)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nenter next/submit | esc cancel")
	return b.String()
}

func (m state) loadCmd() tea.Cmd {
	return func() tea.Msg {
		err := m.svc.Sync(context.Background())
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		channels, err := m.svc.ListChannels(context.Background())
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		experiments, err := m.svc.ListExperiments(context.Background())
		if err != nil {
			return dataLoadedMsg{err: err}
		}
		var messages []model.Message
		if len(channels) > 0 {
			channelID := channels[0].ID
			if selected := m.selectedChannelID(); selected != "" {
				channelID = selected
			}
			messages, err = m.svc.ListMessagesByChannel(context.Background(), channelID)
			if err != nil {
				return dataLoadedMsg{err: err}
			}
		}
		return dataLoadedMsg{channels: channels, experiments: experiments, messages: messages}
	}
}

func syncTickCmd() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg {
		return syncTickMsg{}
	})
}

func (m state) reloadSelectedChannel() tea.Cmd {
	channelID := m.selectedChannelID()
	if channelID == "" {
		return nil
	}
	return func() tea.Msg {
		messages, err := m.svc.ListMessagesByChannel(context.Background(), channelID)
		return dataLoadedMsg{channels: m.currentChannels(), experiments: m.experiments, messages: messages, err: err}
	}
}

func (m state) sendDraftCmd() (tea.Cmd, error) {
	draft := strings.TrimSpace(m.composer.Value())
	channelID := m.selectedChannelID()
	replyTo := m.replyTo
	if draft == "" || channelID == "" {
		if draft == "" {
			return nil, fmt.Errorf("message is empty")
		}
		return nil, fmt.Errorf("no channel selected")
	}
	if strings.TrimSpace(m.defaults.UserName) == "" {
		return nil, fmt.Errorf("user is not configured")
	}
	return func() tea.Msg {
		err := m.svc.SendMessage(context.Background(), app.SendMessageInput{
			UserID:    m.defaults.UserName,
			ChannelID: channelID,
			Subject:   draft,
			ReplyTo:   replyTo,
		})
		status := ""
		if err == nil {
			status = "message sent"
		}
		return actionDoneMsg{status: status, err: err}
	}, nil
}

func (m state) submitFormCmd() tea.Cmd {
	f := *m.form
	f.save()
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		status := f.title + " completed"
		switch f.action {
		case "create-user":
			err = m.svc.CreateUser(ctx, strings.TrimSpace(f.fields[0].value), "")
		case "create-channel":
			err = m.svc.CreateChannel(ctx, strings.TrimSpace(f.fields[0].value), m.defaults.UserName, strings.TrimSpace(f.fields[1].value))
		case "add-member":
			err = m.svc.AddChannelMember(ctx, strings.TrimSpace(f.fields[0].value), m.defaults.UserName, strings.TrimSpace(f.fields[1].value))
		case "create-experiment":
			err = m.svc.CreateExperiment(ctx, strings.TrimSpace(f.fields[0].value), m.defaults.UserName, strings.TrimSpace(f.fields[1].value), strings.TrimSpace(f.fields[2].value), strings.TrimSpace(f.fields[3].value))
		case "retain-attempt":
			err = m.svc.RetainExperimentAttempt(ctx, strings.TrimSpace(f.fields[0].value), strings.TrimSpace(f.fields[1].value))
		default:
			err = fmt.Errorf("unknown form action %q", f.action)
		}
		if err != nil {
			status = ""
		}
		return actionDoneMsg{status: status, err: err}
	}
}

func newForm(title, action string, fields []field) *form {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 2048
	input.SetValue(fields[0].value)
	input.Focus()
	return &form{title: title, action: action, fields: fields, input: input}
}

func (f *form) save() {
	f.fields[f.index].value = f.input.Value()
}

func (f *form) load() {
	f.input.SetValue(f.fields[f.index].value)
}

func (m *state) setChannels(channels []model.Channel) {
	prev := m.selectedChannelID()
	items := make([]list.Item, 0, len(channels))
	selected := 0
	for i, ch := range channels {
		items = append(items, channelItem{channel: ch})
		if ch.ID == prev {
			selected = i
		}
	}
	m.channelList.SetItems(items)
	if len(items) > 0 {
		m.channelList.Select(selected)
	}
}

func (m *state) renderMessages() {
	var lines []string
	for _, msg := range m.messages {
		head := lipgloss.NewStyle().Bold(true).Render(msg.UserID) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(short(msg.CommitHash)) + " " +
			lipgloss.NewStyle().Foreground(lipgloss.Color("247")).Render(msg.CreatedAt.UTC().Format("2006-01-02 15:04:05"))
		if msg.ReplyTo != "" {
			head += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Render("reply:"+short(msg.ReplyTo))
		}
		lines = append(lines, head)
		lines = append(lines, "  "+msg.Subject)
		if strings.TrimSpace(msg.Body) != "" {
			for _, line := range strings.Split(strings.TrimSpace(msg.Body), "\n") {
				lines = append(lines, "  "+line)
			}
		}
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		lines = []string{"No messages yet."}
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func (m *state) resize() {
	leftWidth := max(24, m.width/4)
	rightWidth := max(30, m.width-leftWidth-2)
	composerHeight := 3
	headerHeight := 6
	helpHeight := 3
	messageHeight := max(8, m.height-headerHeight-helpHeight-composerHeight)
	m.channelList.SetSize(leftWidth, max(8, m.height-headerHeight-helpHeight))
	m.viewport.Width = rightWidth - 2
	m.viewport.Height = messageHeight
	m.composer.Width = rightWidth - 4
}

func (m state) currentChannels() []model.Channel {
	items := m.channelList.Items()
	out := make([]model.Channel, 0, len(items))
	for _, item := range items {
		out = append(out, item.(channelItem).channel)
	}
	return out
}

func (m state) selectedChannelID() string {
	item, ok := m.channelList.SelectedItem().(channelItem)
	if !ok {
		return ""
	}
	return item.channel.ID
}

func (m state) selectedMessage() *model.Message {
	if len(m.messages) == 0 {
		return nil
	}
	return &m.messages[len(m.messages)-1]
}

func (m state) focusLabel() string {
	if m.focus == focusChannels {
		return "channels"
	}
	return "composer"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func short(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
