// Package web embebe la interfaz compilada (web/dist) en el binario de haro.
// Compílala con `make web` antes de `make build`; si no existe, se sirve una
// página provisional que permite probar el API local.
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

// FS devuelve los archivos de la interfaz listos para servir.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return sub
		}
	}
	return fstest.MapFS{"index.html": {Data: placeholder}}
}
