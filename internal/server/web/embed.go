package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var embedFS embed.FS

// GetFileSystem returns the embedded file system for serving web assets
func GetFileSystem() http.FileSystem {
	// Try to get the built dist directory
	if distFS, err := fs.Sub(embedFS, "dist"); err == nil {
		return http.FS(distFS)
	}
	
	// Fallback to serving from root (for development)
	return http.FS(embedFS)
}

// GetHandler returns an HTTP handler for serving web assets
func GetHandler() http.Handler {
	return http.FileServer(GetFileSystem())
}