package main

import (
	"os/exec"
	"runtime"
)

// openInFileExplorer opens the platform's native file manager at dir. Used by
// the cowork settings panel ("Open PPT template dir" button). Errors are
// returned to the caller so the UI can surface them.
func openInFileExplorer(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", dir).Start()
	case "darwin":
		return exec.Command("open", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}
