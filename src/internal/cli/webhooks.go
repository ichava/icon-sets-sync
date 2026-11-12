package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/web"
	"github.com/ichava/icon-sets-sync/src/internal/webhooks"
)

func newWebhooksCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Add, list, remove, rotate and test webhook endpoints",
		Long: `Add, list, remove, rotate and test webhook endpoints.

Endpoints live in the global config's webhooks.endpoints; each names the event
types it receives. Their secrets are generated here, shown once, and kept in
webhook-secrets.json in the state directory (mode 0600), never in the config.
A running serve delivers only while webhooks.enabled is on, and reads the
endpoints when it starts. See docs/tools/webhooks.md.`,
	}
	cmd.AddCommand(newWebhooksAddCommand(), newWebhooksListCommand(), newWebhooksRemoveCommand(),
		newWebhooksRotateCommand(), newWebhooksTestCommand())
	return cmd
}

func webhookStateDir() (string, error) {
	dir := web.DefaultStateDir(os.LookupEnv)
	if dir == "" {
		return "", errors.New("no state directory; set XDG_STATE_HOME")
	}
	return dir, nil
}

func webhookSecrets() (*webhooks.Secrets, string, error) {
	dir, err := webhookStateDir()
	if err != nil {
		return nil, "", err
	}
	return &webhooks.Secrets{Path: webhooks.DefaultSecretsPath(dir)}, dir, nil
}

// globalEndpoints is webhooks.endpoints as the global config holds it: the
// list add and remove rewrite. A value from the environment or a flag is
// left alone, and said so.
func globalEndpoints() []settings.Webhook {
	if v, ok := state.stack.Global["webhooks.endpoints"].([]settings.Webhook); ok {
		return v
	}
	return []settings.Webhook{}
}

func warnIfEndpointsOverridden() {
	if o := state.run.Origin("webhooks.endpoints"); o == settings.LayerEnv || o == settings.LayerFlag {
		ui.Warning("webhooks.endpoints is set by %s, which overrides the global config this command writes", o)
	}
}

func newWebhooksAddCommand() *cobra.Command {
	var url, secretEnv string
	var types []string
	cmd := &cobra.Command{
		Use:   "add <name> --url <https URL> --event <type>...",
		Short: "Add an endpoint and show its new secret once",
		Example: "  icon-sets-sync webhooks add ci --url https://ci.example.com/hooks/icons \\\n" +
			"      --event job.finished --event pack.behind_upstream",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			list := globalEndpoints()
			if _, err := settings.FindWebhook(list, name); err == nil {
				return usageErr("webhook %q exists; remove it first, or `webhooks rotate %s` for a new secret", name, name)
			}
			w := settings.Webhook{Name: name, URL: url, Events: types, SecretEnv: secretEnv}
			next := append(append([]settings.Webhook(nil), list...), w)
			k, _ := settings.Lookup("webhooks.endpoints")
			if _, err := k.Check(next); err != nil {
				return usageErr("%s", err)
			}
			for _, t := range types {
				if events.StreamOnly(t) {
					return usageErr("%s is sent on the event stream only, never to a webhook", t)
				}
			}
			policy := newSender("", state.run).Policy
			if _, err := policy.CheckURL(url); err != nil {
				return usageErr("%s", err)
			}
			secrets, _, err := webhookSecrets()
			if err != nil {
				return failure(err)
			}
			if err := web.SaveGlobalSetting(configEngine(), "webhooks.endpoints", next); err != nil {
				return failure(err)
			}
			warnIfEndpointsOverridden()
			if secrets.Has(name) {
				ui.Info("%s keeps the secret it already had; `webhooks rotate %s` makes a new one", name, name)
			} else {
				secret, err := secrets.Create(name)
				if err != nil {
					return failure(err)
				}
				if ui.JSONMode() {
					return ui.JSON(map[string]any{"name": name, "secret": secret})
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), secret)
				ui.Success("added %s; its secret is above, shown once: give it to the receiver", name)
			}
			if secretEnv != "" {
				ui.Info("%s, when set, signs instead of the generated secret", secretEnv)
			}
			if !state.run.Bool("webhooks.enabled") {
				ui.Warning("webhooks.enabled is off, so nothing is delivered yet; `webhooks test %s` sends one now", name)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&url, "url", "", "the receiver: https, or http://localhost with webhooks.allow_insecure_localhost (required)")
	f.StringArrayVar(&types, "event", nil, "an event type to send (repeatable, at least one): "+strings.Join(events.Types(), ", "))
	f.StringVar(&secretEnv, "secret-env", "", "an environment variable whose value, when set, signs instead of the generated secret")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}

func newWebhooksListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List endpoints, the events each receives, and their state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			secrets, dir, err := webhookSecrets()
			if err != nil {
				return failure(err)
			}
			list := state.run.Webhooks("webhooks.endpoints")
			type row struct {
				Name      string   `json:"name"`
				URL       string   `json:"url"`
				Events    []string `json:"events"`
				Secret    string   `json:"secret"`
				Disabled  bool     `json:"disabled"`
				SecretEnv string   `json:"secret_env,omitempty"`
			}
			var rows []row
			for _, w := range list {
				src := "none"
				switch {
				case w.SecretEnv != "" && os.Getenv(w.SecretEnv) != "":
					src = "$" + w.SecretEnv
				case secrets.Has(w.Name):
					src = "generated"
				}
				rows = append(rows, row{w.Name, w.URL, w.Events, src, webhooks.Disabled(dir, w.Name), w.SecretEnv})
			}
			if ui.JSONMode() {
				if rows == nil {
					rows = []row{}
				}
				return ui.JSON(map[string]any{"enabled": state.run.Bool("webhooks.enabled"), "endpoints": rows})
			}
			if len(rows) == 0 {
				ui.Info("no webhook endpoints; `icon-sets-sync webhooks add` makes one")
				return nil
			}
			table := make([][]string, 0, len(rows))
			for _, r := range rows {
				st := "on"
				if r.Disabled {
					st = "disabled"
				}
				table = append(table, []string{r.Name, r.URL, strings.Join(r.Events, ","), r.Secret, st})
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), ui.Table([]string{"NAME", "URL", "EVENTS", "SECRET", "STATE"}, table))
			if !state.run.Bool("webhooks.enabled") {
				ui.Info("webhooks.enabled is off: nothing is delivered")
			}
			return nil
		},
	}
}

func newWebhooksRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an endpoint from the global config, and its generated secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			list := globalEndpoints()
			if _, err := settings.FindWebhook(list, name); err != nil {
				return usageErr("%s in the global config", err)
			}
			var next []settings.Webhook
			for _, w := range list {
				if w.Name != name {
					next = append(next, w)
				}
			}
			var value any = next
			if len(next) == 0 {
				value = nil
			}
			if err := web.SaveGlobalSetting(configEngine(), "webhooks.endpoints", value); err != nil {
				return failure(err)
			}
			warnIfEndpointsOverridden()
			secrets, _, err := webhookSecrets()
			if err != nil {
				return failure(err)
			}
			if err := secrets.Remove(name); err != nil {
				return failure(err)
			}
			ui.Success("removed %s and its secret (the global config's previous copy is in its history)", name)
			return nil
		},
	}
}

func newWebhooksRotateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <name>",
		Short: "Make a new secret; the old one keeps signing for 24 hours",
		Long: `Make a new secret and show it once.

For 24 hours every delivery carries two signatures, one per secret, so the
receiver can switch to the new one with no failed delivery.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, _, err := webhookSecrets()
			if err != nil {
				return failure(err)
			}
			secret, err := secrets.Rotate(args[0])
			if err != nil {
				return usageErr("%s", err)
			}
			until := time.Now().Add(webhooks.RotationGrace)
			if ui.JSONMode() {
				return ui.JSON(map[string]any{"name": args[0], "secret": secret, "previous_valid_until": until})
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), secret)
			ui.Success("rotated %s; the new secret is above, shown once", args[0])
			ui.Info("the old secret keeps signing until %s", until.Local().Format("2006-01-02 15:04"))
			return nil
		},
	}
}

func newWebhooksTestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Send a webhook.test event to one endpoint now",
		Long: `Send a webhook.test event to one endpoint now, whatever events it receives
and whether or not webhooks.enabled is on. A success turns an endpoint that
was disabled after repeated failures back on.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := settings.FindWebhook(state.run.Webhooks("webhooks.endpoints"), args[0])
			if err != nil {
				return usageErr("%s", err)
			}
			dir, err := webhookStateDir()
			if err != nil {
				return failure(err)
			}
			d := webhooks.NewDispatcher(dir, nil, newSender(dir, state.run), nil)
			e, r := d.Test(cmd.Context(), w)
			if ui.JSONMode() {
				out := map[string]any{"event_id": e.ID, "status": r.Status, "delivered": r.Err == nil, "duration_ms": r.Duration.Milliseconds()}
				if r.Err != nil {
					out["error"] = r.Err.Error()
				}
				if err := ui.JSON(out); err != nil {
					return failure(err)
				}
			}
			if r.Err != nil {
				return failure(fmt.Errorf("%s: %w", w.Name, r.Err))
			}
			if !ui.JSONMode() {
				ui.Success("%s answered %d in %s (event %s)", w.Name, r.Status, r.Duration.Round(time.Millisecond), e.ID)
			}
			return nil
		},
	}
}
