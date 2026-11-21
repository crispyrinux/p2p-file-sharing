package web

import (
	"io/fs"
	webassets "p2p-file-sharing/web"
)

// DefaultAssets returns the embedded web assets filesystem.
func DefaultAssets() fs.FS {
	return webassets.Assets
}
