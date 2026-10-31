package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// place and metaFile mirror the JSON that internal/gamemap reads at runtime.
// They are duplicated here so the generator does not depend on the server code
// it is generating data for.
type place struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Kind string  `json:"kind,omitempty"`
}

type metaFile struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Size     float64 `json:"size"`
	Format   string  `json:"format"`
	TileSize int     `json:"tileSize"`
	MinZoom  int     `json:"minZoom"`
	MaxZoom  int     `json:"maxZoom"`
	Names    []place `json:"names"`
}

func writeMeta(path string, meta *metaFile) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
