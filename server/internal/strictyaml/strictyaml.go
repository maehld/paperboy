// Package strictyaml decodes one YAML document with known fields only. The
// instance directory is the configuration (ADR 0037), so a key that no struct
// carries or a second document is an error instead of something ignored.
package strictyaml

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"
)

// Decode reads exactly one document into v. An empty input returns io.EOF, so a
// caller can name what is empty; a second document is an error.
func Decode(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("only one YAML document is allowed")
	}
	return nil
}
