package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	httpapi "github.com/xoBerlierxo/TieBreaker/internal/http"
)

// adminTokenCmd prints a short-lived admin JWT (SPEC §14):
//
//	tiebreaker admin-token [-ttl 15m]
//
// There is no login flow in v1. Whoever can run this command with the server's
// JWT_SECRET is an admin, so keep the secret as safe as a password.
func adminTokenCmd(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("admin-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ttl := fs.Duration("ttl", 15*time.Minute, "token lifetime (max 1h)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	secret := getenv("JWT_SECRET")
	if secret == "" {
		return errors.New("JWT_SECRET is required")
	}
	token, err := httpapi.MintAdminToken(secret, *ttl, time.Now())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, token)
	return err
}
