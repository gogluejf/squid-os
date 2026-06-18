package defaultconfig

import (
	"embed"
)

//go:embed settings.json
//go:embed sys-prompts/*
//go:embed skills/*
var Defaults embed.FS
