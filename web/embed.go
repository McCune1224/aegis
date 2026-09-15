// Package web carries the built control panel into the binary so a deployment
// needs no files on disk.
package web

import (
	"embed"
	"io/fs"
)

// The all: prefix includes the placeholder that keeps the package compiling
// before the bundle exists. make build replaces it with the real app.
//
//go:embed all:dist
var dist embed.FS

// Files is the built app, rooted so index.html sits at the top.
func Files() fs.FS {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded dist is missing: " + err.Error())
	}
	return root
}
