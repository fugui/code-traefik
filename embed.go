package traefik

import "embed"

//go:embed all:frontend/dist
var FrontendFS embed.FS
