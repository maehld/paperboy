// Package duration is the duration type of the configuration and the plans: a
// Go duration string such as "5m" in YAML and JSON.
package duration

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration accepts Go duration strings such as "5m" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(v *yaml.Node) error {
	var s string
	if err := v.Decode(&s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// UnmarshalJSON reads the string form its YAML counterpart uses, so a duration
// survives the round trip through the editor API (ADR 0066, 0070).
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("expected duration as string, e.g. \"5m\"")
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON keeps durations readable in API responses such as /api/profiles.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(Text(time.Duration(d))) }

// Text writes a duration the way operators do: "5m" instead of "5m0s", "2h"
// instead of "2h0m0s", but "1m30s" when the seconds matter.
func Text(d time.Duration) string {
	s := d.String()
	switch {
	case strings.HasSuffix(s, "h0m0s"):
		return strings.TrimSuffix(s, "0m0s")
	case strings.HasSuffix(s, "m0s"):
		return strings.TrimSuffix(s, "0s")
	}
	return s
}
