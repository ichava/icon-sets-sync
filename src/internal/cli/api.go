package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/web"
)

func newAPICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Manage the REST API's bearer tokens",
		Long: `Manage the REST API's bearer tokens.

The API is off until api.enabled is set; serve then answers under /api/v1 for
a request carrying Authorization: Bearer <token>. A token is shown once, when
it is made; only its SHA-256 is kept, in api-tokens.json in the state
directory (mode 0600). See docs/tools/api.md.`,
	}
	token := &cobra.Command{Use: "token", Short: "Create, list and revoke API tokens"}
	token.AddCommand(newAPITokenCreateCommand(), newAPITokenListCommand(), newAPITokenRevokeCommand())
	cmd.AddCommand(token)
	return cmd
}

// tokenStore is the store serve reads.
func tokenStore() (*apitoken.Store, error) {
	dir := web.DefaultStateDir(os.LookupEnv)
	if dir == "" {
		return nil, fmt.Errorf("no state directory; set XDG_STATE_HOME")
	}
	return &apitoken.Store{Path: apitoken.DefaultPath(dir)}, nil
}

func newAPITokenCreateCommand() *cobra.Command {
	var name, scopes, expires string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Make a token and show it once",
		Long: `Make a token and show it once.

--scope is a comma-separated list of read, sync, config:write and admin
(admin carries the other three). --expires takes 30d, 12h or 90m; without it
the token lasts until it is revoked.`,
		Example: "  icon-sets-sync api token create --name ci --scope read,sync --expires 30d",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := apitoken.ParseScopes(scopes)
			if err != nil {
				return usageErr("--scope: %s", err)
			}
			ttl, err := apitoken.ParseTTL(expires)
			if err != nil {
				return usageErr("%s", err)
			}
			store, err := tokenStore()
			if err != nil {
				return failure(err)
			}
			tok, secret, err := store.Create(name, list, ttl)
			if err != nil {
				return usageErr("%s", err)
			}
			if ui.JSONMode() {
				return ui.JSON(map[string]any{"token": secret, "id": tok.ID, "name": tok.Name, "scopes": tok.Scopes, "expires": tok.Expires})
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, secret)
			ui.Success("token %s (%s) created; shown once, keep it now", tok.Name, joinScopes(tok.Scopes))
			if !tok.Expires.IsZero() {
				ui.Info("expires %s", tok.Expires.Format(time.RFC3339))
			}
			ui.Info("only its SHA-256 is kept, in %s", store.Path)
			if !state.run.Bool("api.enabled") {
				ui.Warning("api.enabled is off, so serve has no API yet; set it to use this token")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "a slug naming the token, such as ci (required)")
	f.StringVar(&scopes, "scope", "read", "comma-separated scopes: read, sync, config:write, admin")
	f.StringVar(&expires, "expires", "", "how long it lasts: 30d, 12h, 90m (default: until revoked)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func joinScopes(list []apitoken.Scope) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

func newAPITokenListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tokens: name, scopes, state, expiry and last use (never the token)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := tokenStore()
			if err != nil {
				return failure(err)
			}
			list, err := store.List()
			if err != nil {
				return failure(err)
			}
			now := time.Now()
			if ui.JSONMode() {
				out := make([]map[string]any, 0, len(list))
				for _, t := range list {
					out = append(out, map[string]any{"id": t.ID, "name": t.Name, "scopes": t.Scopes, "state": t.State(now),
						"created": t.Created, "expires": t.Expires, "last_used": t.LastUsed, "revoked": t.Revoked})
				}
				return ui.JSON(out)
			}
			if len(list) == 0 {
				ui.Info("no tokens; `icon-sets-sync api token create --name ci --scope read` makes one")
				return nil
			}
			rows := make([][]string, 0, len(list))
			for _, t := range list {
				rows = append(rows, []string{t.Name, t.ID, joinScopes(t.Scopes), t.State(now), when(t.Expires), when(t.LastUsed)})
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), ui.Table([]string{"NAME", "ID", "SCOPES", "STATE", "EXPIRES", "LAST USED"}, rows))
			return nil
		},
	}
}

func when(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func newAPITokenRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <name|id>",
		Short: "Revoke a token; a running server refuses it from its next request",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			store, err := tokenStore()
			if err != nil {
				return failure(err)
			}
			t, err := store.Revoke(args[0])
			if err != nil {
				return usageErr("%s", err)
			}
			ui.Success("revoked %s (%s)", t.Name, t.ID)
			return nil
		},
	}
}
