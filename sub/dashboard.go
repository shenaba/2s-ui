package sub

import (
	"embed"
	"io/fs"
)

//go:embed dashboard
var subscriberFS embed.FS

// subscriberContent returns the embedded subscriber dashboard filesystem
// rooted at the "dashboard" directory. The panel's build pipeline copies the
// built Vue subscriber app (frontend/subscriber/dist) into sub/dashboard before
// compiling, so the binary always carries a working page. A minimal, valid
// index.html is committed as a fallback so the package still compiles (and the
// endpoint still responds) even when the frontend hasn't been built.
func subscriberContent() (fs.FS, error) {
	return fs.Sub(subscriberFS, "dashboard")
}
