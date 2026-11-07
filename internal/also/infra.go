package also

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// InfraFile marks a directory as portable infrastructure. It records the
	// package name and source harness so "also push" needs no arguments.
	InfraFile   = "also.json"
	SchemaInfra = "also.infra/v1"
)

// Infra is the local marker written by "also init".
type Infra struct {
	Schema    string    `json:"schema"`
	Name      string    `json:"name"`
	Harness   string    `json:"harness,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// FindInfra walks from dir to the filesystem root and returns the first
// directory that contains also.json.
func FindInfra(dir string) (string, *Infra, error) {
	start, err := AbsPath(dir)
	if err != nil {
		return "", nil, err
	}
	for current := start; ; current = filepath.Dir(current) {
		path := filepath.Join(current, InfraFile)
		data, err := os.ReadFile(path)
		if err == nil {
			var in Infra
			if err := json.Unmarshal(data, &in); err != nil {
				return "", nil, fmt.Errorf("parse %s: %w", path, err)
			}
			if in.Schema != SchemaInfra {
				return "", nil, fmt.Errorf("unsupported infrastructure schema %q in %s", in.Schema, path)
			}
			if !ValidName(in.Name) {
				return "", nil, fmt.Errorf("invalid package name %q in %s", in.Name, path)
			}
			if in.Harness != "" {
				if h, ok := ResolveHarness(in.Harness); ok {
					in.Harness = h.ID
				} else {
					return "", nil, fmt.Errorf("unknown harness %q in %s", in.Harness, path)
				}
			}
			return current, &in, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, fmt.Errorf("this directory is not initialized; run: also init <name> [for <harness>]")
		}
	}
}

// SaveInfra writes the marker into dir.
func SaveInfra(dir string, in *Infra) error {
	in.Schema = SchemaInfra
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(dir, InfraFile), data, 0o644)
}
