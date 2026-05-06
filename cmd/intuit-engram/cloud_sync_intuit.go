package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Gentleman-Programming/engram/internal/cloud"
	"github.com/Gentleman-Programming/engram/internal/cloud/cloudstore"
	"github.com/Gentleman-Programming/engram/internal/intuithub"
	"github.com/Gentleman-Programming/engram/internal/product"
)

// cmdCloudSync dispatches `intuit-engram cloud sync <provider>`.
// The only provider supported today is "intuit" (IntuitHub).
func cmdCloudSync() {
	if len(os.Args) < 4 || os.Args[3] == "--help" || os.Args[3] == "-h" || os.Args[3] == "help" {
		fmt.Printf("usage: %s cloud sync <provider>\n", product.Name)
		fmt.Println("supported providers: intuit")
		fmt.Println("")
		fmt.Println("Required env vars (or values in intuit-engram.env next to the binary):")
		fmt.Printf("  %s     IntuitHub-API base URL (e.g. https://devserver01.intuit.ar/intuit-hub-api/)\n", product.EnvIntuitHubBaseURL)
		fmt.Printf("  %s    Admin X-API-Key for the engram-cloud-sync collaborator\n", product.EnvIntuitHubAdminKey)
		fmt.Printf("  %s          Postgres DSN for engram cloud DB\n", product.EnvDatabaseURL)
		fmt.Println("")
		fmt.Println("See intuit-engram.env.example for a template.")
		return
	}

	provider := os.Args[3]
	switch provider {
	case "intuit":
		cmdCloudSyncIntuit()
	default:
		fmt.Fprintf(os.Stderr, "unknown sync provider: %s\n", provider)
		fmt.Fprintln(os.Stderr, "supported providers: intuit")
		exitFunc(1)
	}
}

// cmdCloudSyncIntuit runs a one-shot pull from IntuitHub-API into the cloud
// mirror tables. Surfaces a summary on stdout suitable for scripting.
func cmdCloudSyncIntuit() {
	cfg := cloud.ConfigFromEnv()
	if cfg.IntuitHubBaseURL == "" {
		fmt.Fprintf(os.Stderr, "error: %s is not set\n", product.EnvIntuitHubBaseURL)
		exitFunc(2)
		return
	}
	if cfg.IntuitHubAdminKey == "" {
		fmt.Fprintf(os.Stderr, "error: %s is not set\n", product.EnvIntuitHubAdminKey)
		exitFunc(2)
		return
	}
	if cfg.DSN == "" {
		fmt.Fprintf(os.Stderr, "error: %s is not set (cloud DB DSN required)\n", product.EnvDatabaseURL)
		exitFunc(2)
		return
	}

	cs, err := cloudstore.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect to cloud DB: %v\n", err)
		exitFunc(1)
		return
	}
	defer cs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := intuithub.New(cfg.IntuitHubBaseURL, nil)
	syncer := intuithub.NewSyncer(client, cs, cfg.IntuitHubAdminKey)

	res := syncer.Run(ctx)
	dur := res.FinishedAt.Sub(res.StartedAt)

	if res.Err != nil {
		fmt.Fprintf(os.Stderr, "sync FAILED after %s: %v\n", dur.Round(time.Millisecond), res.Err)
		fmt.Fprintf(os.Stderr, "  partial counts: teams=%d collaborators=%d ownerships=%d\n",
			res.TeamsSynced, res.CollaboratorsSynced, res.OwnershipsSynced)
		exitFunc(1)
		return
	}

	fmt.Printf("sync OK in %s\n", dur.Round(time.Millisecond))
	fmt.Printf("  teams:         %d\n", res.TeamsSynced)
	fmt.Printf("  collaborators: %d\n", res.CollaboratorsSynced)
	fmt.Printf("  ownerships:    %d\n", res.OwnershipsSynced)
}
