// Command mapfetch downloads map imagery and place names from the community map
// at dayz.xam.nu and writes them into this repository.
//
// The web UI embeds whatever lives under web/log-analyzer/maps, so running this
// command is the only moment dayzctl talks to the map host: the server itself
// never does. Re-run it to refresh a map after the map's author publishes an
// update.
//
//	go run ./tools/mapfetch -map nasdara -zooms 3,4
//
// Tiles are cached in the output directory, so an interrupted run resumes and a
// second run only fetches what is missing.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		mapID   = flag.String("map", "nasdara", "map id as used on dayz.xam.nu (nasdara, chernarusplus, livonia, …)")
		zooms   = flag.String("zooms", "3,4", "comma-separated zoom levels to download (0-7)")
		out     = flag.String("out", "web/log-analyzer/maps", "output directory")
		force   = flag.Bool("force", false, "re-download tiles that already exist")
		workers = flag.Int("workers", 2, "parallel tile downloads (keep this low: the CDN drops bursts)")
	)
	flag.Parse()

	levels, err := parseZooms(*zooms)
	if err != nil {
		fail(err)
	}

	if err := run(*mapID, levels, *out, *force, *workers); err != nil {
		fail(err)
	}
}

func run(mapID string, zooms []int, out string, force bool, workers int) error {
	src := newSource(workers)

	fmt.Printf("resolving %s on dayz.xam.nu…\n", mapID)
	rev, err := src.revision(mapID)
	if err != nil {
		return err
	}
	fmt.Printf("  version %s\n", rev)

	cfg, err := src.config(mapID, rev)
	if err != nil {
		return err
	}
	meta := cfg.meta(mapID, rev)
	fmt.Printf("  world %s m, %d place names, tiles %s/%s\n",
		fmtNum(meta.Size), len(meta.Names), cfg.Info.TileSet, cfg.Info.TileVer)

	dir := out + "/" + mapID
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	total := 0
	for _, z := range zooms {
		n := 1 << z
		fetched, skipped := 0, 0
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				path := fmt.Sprintf("%s/%d/%d/%d.%s", dir, z, x, y, meta.Format)
				if !force && fileExists(path) {
					skipped++
					continue
				}
				data, err := src.tile(cfg, z, x, y)
				if err != nil {
					return fmt.Errorf("tile %d/%d/%d: %w", z, x, y, err)
				}
				if err := writeFile(path, data); err != nil {
					return err
				}
				fetched++
			}
		}
		total += fetched
		fmt.Printf("  zoom %d: %d tiles fetched, %d already present (%dx%d)\n", z, fetched, skipped, n, n)
	}

	meta.MinZoom, meta.MaxZoom = zooms[0], zooms[len(zooms)-1]
	if err := writeMeta(dir+"/meta.json", meta); err != nil {
		return err
	}

	fmt.Printf("wrote %s/meta.json and %d new tiles\n", dir, total)
	return nil
}

func parseZooms(raw string) ([]int, error) {
	var levels []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var z int
		if _, err := fmt.Sscanf(part, "%d", &z); err != nil {
			return nil, fmt.Errorf("invalid zoom %q", part)
		}
		if z < 0 || z > 7 {
			return nil, fmt.Errorf("zoom %d out of range (the pyramid stops at 7)", z)
		}
		levels = append(levels, z)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("no zoom levels given")
	}
	return levels, nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "mapfetch: %v\n", err)
	os.Exit(1)
}
