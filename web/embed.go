// Package web embeds the compiled interface (web/dist) in the haro binary.
// Build it with `make web` before `make build`; if it does not exist, a
// placeholder page that allows trying the local API is served.
package web

import (
	"embed"
	"io/fs"
	"testing/fstest"
)

//go:embed all:dist
var dist embed.FS

//go:embed placeholder.html
var placeholder []byte

// FS returns the interface files ready to be served.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return sub
		}
	}
	return fstest.MapFS{"index.html": {Data: placeholder}}
}
