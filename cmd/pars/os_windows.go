//go:build windows

package main

import "syscall"

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWin = kernel32.NewProc("GetConsoleWindow")
	procShowWindow    = user32.NewProc("ShowWindow")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
)

// hideConsole: Sadece Windows'ta terminal penceresini arka planda gizler
func hideConsole() {
	hwnd, _, _ := procGetConsoleWin.Call()
	if hwnd != 0 {
		procShowWindow.Call(hwnd, 0) // 0 = SW_HIDE
	}
}

// initConsole: Sadece Windows'ta çalışır
func initConsole() {
	procAllocConsole.Call()
}