package webui

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

func Embedded() (fs.FS, error) {
	assets, err := fs.Sub(embedded, "dist/client")
	if err != nil {
		return nil, fmt.Errorf("dashboard assets are not embedded; run the web build before compiling: %w", err)
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, fmt.Errorf("dashboard index is not embedded; run the web build before compiling: %w", err)
	}
	return assets, nil
}
