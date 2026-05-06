package main

import (
	"fmt"
	"os"

	"github.com/Gentleman-Programming/engram/internal/store"
)

func main() {
	cfg, _ := store.DefaultConfig()
	s, err := store.New(cfg)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	defer s.Close()

	obs, err := s.AllObservations("", "", 100)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	fmt.Println("ID | Title | CanonicalStatus")
	fmt.Println("---|-------|----------------")
	for _, o := range obs {
		status := o.CanonicalStatus
		if status == "" {
			status = "(empty)"
		}
		fmt.Printf("%2d | %-40s | %s\n", o.ID, o.Title, status)
	}
}
