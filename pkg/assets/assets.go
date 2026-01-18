package assets

import (
	"embed"
	"io/fs"
)

//go:embed embedded-resources/*
var embeddedFiles embed.FS

// GetFileSystem 返回剥离了顶层目录的文件系统，
// 这样访问时直接用 "common-tools/..." 而不是 "embedded-resources/common-tools/..."
func GetFileSystem() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "embedded-resources")
}
