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

	// Simulate MCP search with canonical_status filter
	results, err := s.Search("auth", store.SearchOptions{
		CanonicalStatus: "reviewed,canonical",
	})
	if err != nil {
		fmt.Println("Search error:", err)
		os.Exit(1)
	}

	fmt.Printf("MCP filter (reviewed,canonical): %d results\n", len(results))
	for _, r := range results {
		fmt.Printf("  #%d: %s (status: %s)\n", r.ID, r.Title, r.CanonicalStatus)
	}

	// Without filter
	results2, err := s.Search("auth", store.SearchOptions{})
	if err != nil {
		fmt.Println("Search error:", err)
		os.Exit(1)
	}
	fmt.Printf("\nNo filter: %d results\n", len(results2))
	for _, r := range results2 {
		fmt.Printf("  #%d: %s (status: %s)\n", r.ID, r.Title, r.CanonicalStatus)
	}
}
