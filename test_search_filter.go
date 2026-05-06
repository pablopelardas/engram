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

	// Search with canonical_status filter
	results, err := s.Search("test", store.SearchOptions{
		CanonicalStatus: "reviewed,canonical",
	})
	if err != nil {
		fmt.Println("Search error:", err)
		os.Exit(1)
	}

	fmt.Printf("Search with canonical_status='reviewed,canonical': %d results\n", len(results))
	for _, r := range results {
		fmt.Printf("  #%d: %s (status: %s)\n", r.ID, r.Title, r.CanonicalStatus)
	}

	// Search without filter
	results2, err := s.Search("test", store.SearchOptions{})
	if err != nil {
		fmt.Println("Search error:", err)
		os.Exit(1)
	}
	fmt.Printf("\nSearch without filter: %d results\n", len(results2))
}
