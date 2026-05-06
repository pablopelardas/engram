package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// envFileBaseName is the file we look for next to the binary (or in cwd).
// Override with INTUIT_ENGRAM_ENV_FILE.
const envFileBaseName = "intuit-engram.env"

// envFileOverrideVar lets users point at a non-default location.
const envFileOverrideVar = "INTUIT_ENGRAM_ENV_FILE"

// loadEnvFile loads KEY=VALUE pairs from a config file into the process
// environment, but only when those keys aren't already set. Lookup order:
//
//  1. $INTUIT_ENGRAM_ENV_FILE (if set)
//  2. <dir of executable>/intuit-engram.env
//  3. ./intuit-engram.env (current working directory)
//
// The first existing file wins. Missing files are silently ignored — env
// vars from the shell still work as before. Parse errors are written to
// stderr but do not abort the program.
//
// Format:
//   - KEY=VALUE
//   - blank lines and lines starting with # are ignored
//   - VALUE may be wrapped in single or double quotes; quotes are stripped
//   - no escaping, no variable expansion (keep it boring)
func loadEnvFile() {
	path := resolveEnvFilePath()
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		// Override was set but file doesn't exist → loud failure.
		// Default lookup miss → silent (already handled in resolveEnvFilePath).
		fmt.Fprintf(os.Stderr, "warning: env file %s: %v\n", path, err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			fmt.Fprintf(os.Stderr, "warning: %s line %d: no '=' separator, skipping\n", path, lineNo)
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		// Strip matching surrounding quotes.
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		// Don't override existing env vars: shell wins over file.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s line %d: setenv %s: %v\n", path, lineNo, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s: read error: %v\n", path, err)
	}
}

func resolveEnvFilePath() string {
	if override := strings.TrimSpace(os.Getenv(envFileOverrideVar)); override != "" {
		return override
	}
	// Next to the executable.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), envFileBaseName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Current working directory.
	if _, err := os.Stat(envFileBaseName); err == nil {
		return envFileBaseName
	}
	return ""
}
