//go:build !windows

package main

import "fmt"

// captureLive is Windows-only (it uses PowerShell + System.Drawing). On other
// platforms we return a clear error so `cua-replay -live` fails fast with an
// explanation instead of a build error. Use the replay mode (-image/-dir) there.
func captureLive() ([]byte, error) {
	return nil, fmt.Errorf("live capture is Windows-only; use -image or -dir to replay existing screenshots")
}
