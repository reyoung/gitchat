package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/reyoung/gitchat/app"
	"github.com/reyoung/gitchat/gitrepo"
	"github.com/reyoung/gitchat/gui"
	"github.com/reyoung/gitchat/store"
)

type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "index":
		return cmdIndex(ctx, args[1:])
	case "users":
		return cmdUsers(ctx, args[1:])
	case "channels":
		return cmdChannels(ctx, args[1:])
	case "messages":
		return cmdMessages(ctx, args[1:])
	case "experiments":
		return cmdExperiments(ctx, args[1:])
	case "gui":
		return cmdGUI(ctx, args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Println(`gitchat <command>

Commands:
  index
  users create
  users list
  channels create
  channels add-member
  channels list
  messages send
  messages list --channel <id>
  experiments create
  experiments retain
  experiments list
  gui`)
}

func defaultRepoPath() string {
	return defaultRepoSpec()
}

func openStore(ctx context.Context, dbPath string) (*store.Store, error) {
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func openService(ctx context.Context, repoSpec, dbPath string) (*app.Service, func() error, error) {
	s, err := openStore(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	var repo *gitrepo.Repo
	svc := app.NewService(nil, s)
	if resolvedPath, ok := resolveLocalRepoPath(repoSpec); ok {
		if isBareLocalRepoPath(resolvedPath) {
			repo = gitrepo.NewRemote(resolvedPath)
			svc.RemoteName = "origin"
		} else {
			repo = gitrepo.New(resolvedPath)
			svc.RemoteName = ""
		}
	} else {
		repo = gitrepo.NewRemote(repoSpec)
		svc.RemoteName = "origin"
	}
	svc.Repo = repo
	return svc, s.Close, nil
}

func cmdIndex(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
	dbPath := fs.String("db", "", "path to cache db")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := resolveOptions(ctx, *repoPath, *dbPath)
	if err != nil {
		return err
	}
	svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := svc.Sync(ctx); err != nil {
		return err
	}
	fmt.Println("indexed", opts.RepoSpec)
	return nil
}

func cmdUsers(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gitchat users <create|list>")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("users create", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		userID := fs.String("user", "", "user id")
		keyPath := fs.String("key", "", "public key file path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.CreateUser(ctx, resolveUserName(*userID, opts), resolveKeyPath(*keyPath, opts))
	case "list":
		fs := flag.NewFlagSet("users list", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		s, err := openStore(ctx, opts.DBPath)
		if err != nil {
			return err
		}
		defer s.Close()
		users, err := s.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, user := range users {
			fmt.Printf("%s\t%s\n", user.ID, user.Branch)
		}
		return nil
	default:
		return fmt.Errorf("usage: gitchat users <create|list>")
	}
}

func cmdChannels(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gitchat channels <create|add-member|list>")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("channels create", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		channelID := fs.String("channel", "", "channel id")
		creator := fs.String("creator", "", "creator user id")
		title := fs.String("title", "", "channel title")
		public := fs.Bool("public", false, "create a public channel")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.CreateChannel(ctx, *channelID, resolveUserName(*creator, opts), *title, *public)
	case "add-member":
		fs := flag.NewFlagSet("channels add-member", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		channelID := fs.String("channel", "", "channel id")
		actor := fs.String("actor", "", "actor user id")
		member := fs.String("member", "", "member user id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.AddChannelMember(ctx, *channelID, resolveUserName(*actor, opts), *member)
	case "list":
		fs := flag.NewFlagSet("channels list", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		s, err := openStore(ctx, opts.DBPath)
		if err != nil {
			return err
		}
		defer s.Close()
		channels, err := s.ListChannels(ctx)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			visibility := "private"
			if channel.IsPublic {
				visibility = "public"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", channel.ID, channel.Creator, channel.Title, visibility, channel.Branch)
		}
		return nil
	default:
		return fmt.Errorf("usage: gitchat channels <create|add-member|list>")
	}
}

func cmdExperiments(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gitchat experiments <create|retain|list>")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("experiments create", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		experimentID := fs.String("experiment", "", "experiment id")
		actor := fs.String("actor", "", "actor user id")
		title := fs.String("title", "", "experiment title")
		baseRef := fs.String("base", "HEAD", "base ref for experiment branch")
		configJSON := fs.String("config", "{}", "config json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.CreateExperiment(ctx, *experimentID, resolveUserName(*actor, opts), *title, *baseRef, *configJSON)
	case "retain":
		fs := flag.NewFlagSet("experiments retain", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		experimentID := fs.String("experiment", "", "experiment id")
		ref := fs.String("ref", "", "attempt sha or branch")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.RetainExperimentAttempt(ctx, *experimentID, *ref)
	case "list":
		fs := flag.NewFlagSet("experiments list", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		s, err := openStore(ctx, opts.DBPath)
		if err != nil {
			return err
		}
		defer s.Close()
		experiments, err := s.ListExperiments(ctx)
		if err != nil {
			return err
		}
		for _, experiment := range experiments {
			fmt.Printf("%s\t%s\t%s\t%s\n", experiment.ID, experiment.Creator, experiment.Title, experiment.Branch)
		}
		return nil
	default:
		return fmt.Errorf("usage: gitchat experiments <create|retain|list>")
	}
}

func cmdMessages(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gitchat messages <send|list>")
	}
	switch args[0] {
	case "send":
		fs := flag.NewFlagSet("messages send", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
		dbPath := fs.String("db", "", "path to cache db")
		userID := fs.String("user", "", "user id")
		channelID := fs.String("channel", "", "channel id")
		subject := fs.String("subject", "", "message subject")
		body := fs.String("body", "", "message body")
		replyTo := fs.String("reply-to", "", "reply target commit hash")
		experimentID := fs.String("experiment", "", "experiment id")
		experimentSHA := fs.String("experiment-sha", "", "experiment commit hash")
		var follows stringListFlag
		var attachments stringListFlag
		fs.Var(&follows, "follow", "followed commit hash, repeatable")
		fs.Var(&attachments, "attach", "attachment path, repeatable")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
		if err != nil {
			return err
		}
		defer closeFn()
		return svc.SendMessage(ctx, app.SendMessageInput{
			UserID:        resolveUserName(*userID, opts),
			ChannelID:     *channelID,
			Subject:       *subject,
			Body:          *body,
			ReplyTo:       *replyTo,
			Follows:       []string(follows),
			ExperimentID:  *experimentID,
			ExperimentSHA: *experimentSHA,
			Attachments:   []string(attachments),
		})
	case "list":
		fs := flag.NewFlagSet("messages list", flag.ContinueOnError)
		repoPath := fs.String("repo", defaultRepoPath(), "repo URL or SSH repo spec")
		dbPath := fs.String("db", "", "path to cache db")
		channelID := fs.String("channel", "", "channel id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*channelID) == "" {
			return fmt.Errorf("--channel is required")
		}
		opts, err := resolveOptions(ctx, *repoPath, *dbPath)
		if err != nil {
			return err
		}
		s, err := openStore(ctx, opts.DBPath)
		if err != nil {
			return err
		}
		defer s.Close()
		messages, err := s.ListMessagesByChannel(ctx, *channelID)
		if err != nil {
			return err
		}
		for _, message := range messages {
			fmt.Printf("%s\t%s\t%s\t%s\n", message.CommitHash, message.UserID, message.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), message.Subject)
		}
		return nil
	default:
		return fmt.Errorf("usage: gitchat messages <send|list>")
	}
}

func cmdGUI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	repoPath := fs.String("repo", defaultRepoPath(), "repo URL, SSH repo spec, or local git repo path")
	dbPath := fs.String("db", "", "path to cache db")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts, err := resolveOptions(ctx, *repoPath, *dbPath)
	if err != nil {
		return err
	}
	svc, closeFn, err := openService(ctx, opts.RepoSpec, opts.DBPath)
	if err != nil {
		return err
	}
	defer closeFn()
	return gui.Run(ctx, svc, gui.Defaults{UserName: opts.UserName})
}
