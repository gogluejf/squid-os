package defaultconfig

import (
	"embed"
)

//go:embed settings.json
//go:embed sys-prompts/*
//go:embed skills/*
//go:embed agents/*
var Defaults embed.FS
