package assets

import "embed"

//go:embed templates/* static/* static/vendor/*
var FS embed.FS
