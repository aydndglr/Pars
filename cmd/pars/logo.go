package main

import (
	"fmt"
	"strings"
)

func printFixedHeader() {
	neonGreen := "\033[92m"
	reset := "\033[0m"
	logo := "Proaktif Akıl [PA]🐯[RS] Rasyonel Sistem"

	fmt.Print("\033[H\033[2J")
	fmt.Printf("%s%s%s\n", neonGreen, logo, reset)
	fmt.Println("\033[90m--------------------------------------------------\033[0m")

	SetTaskStatus("") 
}

func SetTaskStatus(taskName string) {
	if taskName == "" {
		fmt.Print("\033]0;[PA]🐯[RS] - Beklemede...\007")
		return
	}

	title := fmt.Sprintf("[PA]🐯[RS] - ⚡ AKTİF GÖREV: %s", taskName)
	fmt.Printf("\033]0;%s\007", title)
	fmt.Printf("\n\033[43;30m ⚡ DEVAM EDEN GÖREV \033[0m \033[93m%s\033[0m\n", taskName)
	fmt.Println("\033[90m" + strings.Repeat("-", 50) + "\033[0m")
}