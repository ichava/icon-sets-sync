package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/events"
)

// webhookNameRE is an endpoint's name: a slug, so it is safe in a URL path,
// a file key and a log line.
var webhookNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// envNameRE is a portable environment variable name.
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// coerceWebhooks checks webhooks.endpoints: an array of objects, each with a
// slug name, an http(s) URL, at least one known event type and, optionally,
// the name of the environment variable holding its secret. The full URL
// policy (https only, the deny ranges) is the webhooks package's, applied at
// delivery, so a value read here is well formed rather than allowed.
func coerceWebhooks(value any) (any, error) {
	var items []any
	switch x := value.(type) {
	case []Webhook:
		data, err := json.Marshal(x)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
	case []any:
		items = x
	case nil:
		return []Webhook{}, nil
	default:
		return nil, fmt.Errorf("want an array of webhook endpoints, got %T", value)
	}
	out := make([]Webhook, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("endpoint %d: want an object, got %T", i, item)
		}
		w, err := webhookFrom(obj)
		if err != nil {
			return nil, fmt.Errorf("endpoint %d: %w", i, err)
		}
		if seen[w.Name] {
			return nil, fmt.Errorf("endpoint %d: the name %q is used twice", i, w.Name)
		}
		seen[w.Name] = true
		out = append(out, w)
	}
	return out, nil
}

func webhookFrom(obj map[string]any) (Webhook, error) {
	var w Webhook
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch k {
		case "name", "url", "secret_env":
			s, ok := obj[k].(string)
			if !ok {
				return w, fmt.Errorf("%s: want a string, got %T", k, obj[k])
			}
			switch k {
			case "name":
				w.Name = s
			case "url":
				w.URL = s
			default:
				w.SecretEnv = s
			}
		case "events":
			list, ok := obj[k].([]any)
			if !ok {
				return w, fmt.Errorf("events: want an array of event types, got %T", obj[k])
			}
			for _, e := range list {
				s, ok := e.(string)
				if !ok {
					return w, fmt.Errorf("events: want strings, got %T", e)
				}
				w.Events = append(w.Events, s)
			}
		default:
			return w, fmt.Errorf("unknown field %q (known: name, url, events, secret_env)", k)
		}
	}
	if !webhookNameRE.MatchString(w.Name) {
		return w, fmt.Errorf("name %q: want a slug such as ci-notify", w.Name)
	}
	u, err := url.Parse(w.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return w, fmt.Errorf("%s: url %q is not an http(s) URL", w.Name, w.URL)
	}
	if u.User != nil {
		return w, fmt.Errorf("%s: url must not carry credentials; the signature authenticates the sender", w.Name)
	}
	if len(w.Events) == 0 {
		return w, fmt.Errorf("%s: events is required; list the event types to send (no endpoint receives everything implicitly)", w.Name)
	}
	var unknown []string
	for _, e := range w.Events {
		if !events.Known(e) {
			unknown = append(unknown, e)
		}
	}
	if len(unknown) > 0 {
		return w, fmt.Errorf("%s: unknown event type(s) %s (known: %s)", w.Name, strings.Join(unknown, ", "), strings.Join(events.Types(), ", "))
	}
	if w.SecretEnv != "" && !envNameRE.MatchString(w.SecretEnv) {
		return w, fmt.Errorf("%s: secret_env %q is not an environment variable name", w.Name, w.SecretEnv)
	}
	return w, nil
}

// ErrNoWebhook is returned for a webhook name webhooks.endpoints lacks.
var ErrNoWebhook = errors.New("no such webhook")

// FindWebhook returns the endpoint with this name.
func FindWebhook(list []Webhook, name string) (Webhook, error) {
	for _, w := range list {
		if w.Name == name {
			return w, nil
		}
	}
	return Webhook{}, fmt.Errorf("%w %q", ErrNoWebhook, name)
}
