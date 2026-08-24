package sub

import (
	"embed"
	"io/fs"
)

//go:embed dashboard
var subscriberFS embed.FS

// subscriberIndex returns the subscriber dashboard page. The build pipeline
// copies the built Vue app (frontend/subscriber/dist) into sub/dashboard, so
// index.html is the real dashboard whenever that step ran.
//
// fallback.html is the committed stand-in, and it is a separate name on
// purpose: it keeps everything the build emits ignorable, so building never
// leaves a tracked file modified. It also means this package compiles — and
// the endpoint answers — from a bare checkout, which is what CI builds from.
func subscriberIndex() ([]byte, error) {
	page, err := subscriberFS.ReadFile("dashboard/index.html")
	if err == nil {
		return page, nil
	}
	return subscriberFS.ReadFile("dashboard/fallback.html")
}

// subscriberAssets returns the dashboard's bundled assets. The directory only
// exists once the subscriber frontend has been built; when it doesn't, every
// lookup under it simply 404s — the same thing the panel does with an unbuilt
// web/html, and the reason a fallback page must stay self-contained.
func subscriberAssets() (fs.FS, error) {
	return fs.Sub(subscriberFS, "dashboard/assets")
}
