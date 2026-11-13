package settings

import (
	"strings"
	"testing"
)

func TestWebhookEndpointsAreChecked(t *testing.T) {
	good := `{"webhooks": {"endpoints": [{"name": "ci", "url": "https://ci.example.com/hook", "events": ["job.finished"], "secret_env": "CI_HOOK_SECRET"}]}}`
	v, err := ParseGlobal("/c.json", []byte(good))
	if err != nil {
		t.Fatal(err)
	}
	list := v["webhooks.endpoints"].([]Webhook)
	if len(list) != 1 || list[0].Name != "ci" || list[0].SecretEnv != "CI_HOOK_SECRET" {
		t.Fatalf("%+v", list)
	}
	for name, bad := range map[string]string{
		"no events":     `[{"name": "ci", "url": "https://ci.example.com/hook", "events": []}]`,
		"unknown event": `[{"name": "ci", "url": "https://ci.example.com/hook", "events": ["job.exploded"]}]`,
		"a secret":      `[{"name": "ci", "url": "https://ci.example.com/hook", "events": ["job.finished"], "secret": "whsec_x"}]`,
		"no scheme":     `[{"name": "ci", "url": "ci.example.com/hook", "events": ["job.finished"]}]`,
		"credentials":   `[{"name": "ci", "url": "https://u:p@ci.example.com/hook", "events": ["job.finished"]}]`,
		"bad name":      `[{"name": "CI hook", "url": "https://ci.example.com/hook", "events": ["job.finished"]}]`,
		"twice":         `[{"name": "ci", "url": "https://a.example.com", "events": ["job.finished"]}, {"name": "ci", "url": "https://b.example.com", "events": ["job.finished"]}]`,
		"bad env":       `[{"name": "ci", "url": "https://ci.example.com/hook", "events": ["job.finished"], "secret_env": "A-B"}]`,
	} {
		k, _ := Lookup("webhooks.endpoints")
		if _, err := k.ParseString(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	got, err := FromEnv(func(k string) (string, bool) {
		if k == "ICHAVA_WEBHOOKS_ENDPOINTS" {
			return `[{"name": "ci", "url": "https://ci.example.com/hook", "events": ["pack.synced"]}]`, true
		}
		return "", false
	})
	if err != nil || len(got["webhooks.endpoints"].([]Webhook)) != 1 {
		t.Fatalf("from the environment: %v, %v", got, err)
	}
	r := Resolve(LayerValues{LayerGlobal, v})
	if !strings.Contains(FormatValue(r, "webhooks.endpoints"), `"name":"ci"`) {
		t.Fatal("FormatValue")
	}
	if _, err := FindWebhook(r.Webhooks("webhooks.endpoints"), "nope"); err == nil {
		t.Fatal("FindWebhook found a missing name")
	}
}
