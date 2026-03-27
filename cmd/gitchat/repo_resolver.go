package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func defaultRepoSpec() string {
	return ""
}

func resolveRepoSpec(ctx context.Context, spec string) (string, error) {
	_ = ctx
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("repo is required; pass --repo or define it in ~/.gitchat")
	}
	if isRepoSpec(spec) {
		return spec, nil
	}
	localPath, ok := resolveLocalRepoPath(spec)
	if ok {
		return localPath, nil
	}
	return "", fmt.Errorf("--repo must be a repo URL, SSH repo spec, or a local git repo path")
}

func isRepoSpec(spec string) bool {
	if strings.Contains(spec, "://") {
		return true
	}
	at := strings.Index(spec, "@")
	colon := strings.Index(spec, ":")
	slash := strings.Index(spec, "/")
	if at > 0 && colon > at && (slash == -1 || colon < slash) {
		return true
	}
	return false
}

func resolveLocalRepoPath(spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	path := spec
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		path = filepath.Join(wd, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if isGitDir(path) || isGitDir(filepath.Join(path, ".git")) {
		return path, true
	}
	return "", false
}

func isGitDir(path string) bool {
	headInfo, err := os.Stat(filepath.Join(path, "HEAD"))
	if err != nil || headInfo.IsDir() {
		return false
	}
	objectsInfo, err := os.Stat(filepath.Join(path, "objects"))
	if err != nil || !objectsInfo.IsDir() {
		return false
	}
	refsInfo, err := os.Stat(filepath.Join(path, "refs"))
	if err != nil || !refsInfo.IsDir() {
		return false
	}
	return true
}
