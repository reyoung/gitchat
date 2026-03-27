package model

import "time"

type User struct {
	ID        string
	Branch    string
	AvatarURL string
}

type Channel struct {
	ID       string
	Branch   string
	Creator  string
	Title    string
	IsPublic bool
}

type Experiment struct {
	ID      string
	Branch  string
	Creator string
	Title   string
}

type Message struct {
	CommitHash    string
	UserID        string
	Branch        string
	ChannelID     string
	Subject       string
	Body          string
	ReplyTo       string
	EditOf        string
	DeleteOf      string
	Follows       []string
	ExperimentID  string
	ExperimentSHA string
	CreatedAt     time.Time
}

type RefState struct {
	Name     string
	HeadHash string
}

type ChannelEvent struct {
	CommitHash string
	Branch     string
	ChannelID  string
	EventType  string
	Actor      string
	Member     string
	CreatedAt  time.Time
}

type ExperimentEvent struct {
	CommitHash   string
	Branch       string
	ExperimentID string
	EventType    string
	Actor        string
	RetainedSHA  string
	CreatedAt    time.Time
}
