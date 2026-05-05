package store

import (
	"os"
	"os/exec"
	"strings"
)

// DetectAuthor returns the best-effort author string for observations.
// It tries these sources in order:
//  1. INTUIT_ENGRAM_AUTHOR environment variable
//  2. git config user.name + user.email
//  3. USER / USERNAME environment variable
//  4. "unknown" as final fallback
func DetectAuthor() string {
	// 1. Explicit override via environment variable
	if author := os.Getenv("INTUIT_ENGRAM_AUTHOR"); author != "" {
		return strings.TrimSpace(author)
	}

	// 2. Git config (user.name + user.email)
	name := gitConfig("user.name")
	email := gitConfig("user.email")
	if name != "" && email != "" {
		return name + " <" + email + ">"
	}
	if name != "" {
		return name
	}
	if email != "" {
		return email
	}

	// 3. System user
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}

	return "unknown"
}

func gitConfig(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
