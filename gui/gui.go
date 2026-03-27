package gui

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/reyoung/gitchat/app"
)

//go:embed frontend/dist/*
var assets embed.FS

type Defaults struct {
	UserName string
}

func Run(_ context.Context, svc *app.Service, defaults Defaults) error {
	bridge := NewBridge(svc, defaults)
	return wails.Run(&options.App{
		Title:            "GitChat",
		Width:            1440,
		Height:           920,
		MinWidth:         1100,
		MinHeight:        720,
		DisableResize:    false,
		Frameless:        false,
		BackgroundColour: &options.RGBA{R: 22, G: 24, B: 31, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: bridge.startup,
		Bind: []interface{}{
			bridge,
		},
	})
}
