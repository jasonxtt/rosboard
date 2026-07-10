package ui

import (
	"embed"
	"io/fs"
)

//go:embed dist dist/*
var rawAssets embed.FS

func Assets() (fs.FS, error) {
	return fs.Sub(rawAssets, "dist")
}
