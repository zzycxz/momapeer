//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installmomapeerSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installmomapeerSystemQuitHook()
	})
}

//export momapeerMarkSystemQuit
func momapeerMarkSystemQuit() {
	markSystemQuitRequested()
}
