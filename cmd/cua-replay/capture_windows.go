//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"os/exec"
)

// captureLive grabs the full primary screen on Windows. We shell out to
// PowerShell (System.Windows.Forms + System.Drawing) rather than pull in the
// golang.org/x/sys windows syscall surface, so this stays a thin, dependency-free
// helper. The captured PNG is returned as bytes. This is the LIVE mode of
// cua-replay: see exactly what the agent would see on the current desktop, right
// now, without running the full agent loop.
func captureLive() ([]byte, error) {
	// PowerShell script: capture the virtual screen to a temp PNG, return its path.
	// VirtualScreen covers all monitors; CopyFromScreen(0,0) of the full bounds.
	script := `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $b=[System.Windows.Forms.SystemInformation]::VirtualScreen; $bmp=New-Object System.Drawing.Bitmap($b.Width,$b.Height); $g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.X,$b.Y,0,0,$bmp.Size); $p=[System.IO.Path]::GetTempFileName()+'.png'; $bmp.Save($p,[System.Drawing.Imaging.ImageFormat]::Png); Write-Output $p`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("powershell capture: %w", err)
	}
	path := bytes.TrimSpace(out)
	if len(path) == 0 {
		return nil, fmt.Errorf("powershell returned no path")
	}
	data, err := os.ReadFile(string(path))
	if err != nil {
		return nil, err
	}
	// Validate it's a real PNG before returning — a failed capture can write a
	// truncated file, and we'd rather fail loudly than feed garbage to the VLM.
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("captured file is not a valid PNG: %w", err)
	}
	_ = os.Remove(string(path))
	return data, nil
}

// base64PNG is unused on Windows but keeps the file self-contained if referenced.
var _ = base64.StdEncoding
