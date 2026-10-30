// Command mapfetch downloads map imagery and place names from the community map
// at dayz.xam.nu and writes them into this repository.
//
// The web UI embeds whatever lives under web/log-analyzer/maps, so running this
// command is the only moment dayzctl talks to the map host: the server itself
// never does. Re-run it to refresh a map after the map's author publishes an
// update.
//
//	go run ./tools/mapfetch -map nasdara -zooms 3,4
//	go run ./tools/mapfetch -all -zooms 3
//
// Tiles are cached in the output directory, so an interrupted run resumes and a
// second run only fetches what is missing.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

func main() {
	var (
		mapID   = flag.String("map", "", "map id as used on the map host (nasdara, chernarusplus, livonia, …)")
		all     = flag.Bool("all", false, "fetch every map the host publishes")
		list    = flag.Bool("list", false, "print the published map ids and exit")
		zooms   = flag.String("zooms", "3", "comma-separated zoom levels to download (0-7)")
		out     = flag.String("out", "web/log-analyzer/maps", "output directory")
		force   = flag.Bool("force", false, "re-download tiles that already exist")
		workers = flag.Int("workers", 2, "parallel requests inside one map")
		par     = flag.Int("parallel", 3, "maps fetched concurrently")
	)
	flag.Parse()

	levels, err := parseZooms(*zooms)
	if err != nil {
		fail(err)
	}

	src := newSource(*workers)

	if *list || *all {
		ids, err := src.mapIDs()
		if err != nil {
			fail(err)
		}
		if *list {
			for _, id := range ids {
				fmt.Println(id)
			}
			return
		}
		fail(fetchAll(src, ids, levels, *out, *force, *par))
		return
	}

	if *mapID == "" {
		fail(fmt.Errorf("nothing to do: pass -map <id>, -all, or -list"))
	}
	fail(fetchOne(src, *mapID, levels, *out, *force))
}

// fetchAll downloads every published map, skipping the ones already complete.
// Maps are fetched a few at a time: a single stream is latency-bound and a full
// run would otherwise take close to an hour.
func fetchAll(src *source, ids []string, zooms []int, out string, force bool, parallel int) error {
	if parallel < 1 {
		parallel = 1
	}
	fmt.Printf("fetching %d maps (zoom %v, %d at a time)\n", len(ids), zooms, parallel)

	var (
		mu     sync.Mutex
		failed []string
		done   int
		sem    = make(chan struct{}, parallel)
		wg     sync.WaitGroup
	)

	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := fetchOne(src, id, zooms, out, force); err != nil {
				fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", id, err)
				mu.Lock()
				failed = append(failed, id)
				mu.Unlock()
				return
			}

			mu.Lock()
			done++
			fmt.Printf("  [%d/%d] %s done\n", done, len(ids), id)
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	if len(failed) > 0 {
		return fmt.Errorf("%d map(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	fmt.Printf("\nall %d maps written to %s\n", len(ids), out)
	return nil
}

func fetchOne(src *source, mapID string, zooms []int, out string, force bool) error {
	rev, err := src.revision(mapID)
	if err != nil {
		return err
	}

	cfg, err := src.config(mapID, rev)
	if err != nil {
		return err
	}
	meta := cfg.meta(mapID, rev)
	fmt.Printf("  version %s · world %s m · %d place names\n", rev, fmtNum(meta.Size), len(meta.Names))

	dir := out + "/" + mapID
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	total, skipped := 0, 0
	for _, z := range zooms {
		n := 1 << z
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
				total++
			}
		}
	}

	// The zoom range reflects what is actually on disk, so a map fetched at
	// several zoom levels over separate runs keeps every level it has.
	minZoom, maxZoom, ok := detectZooms(dir)
	if !ok {
		return fmt.Errorf("no tiles were written")
	}
	meta.MinZoom, meta.MaxZoom = minZoom, maxZoom

	if err := writeMeta(dir+"/meta.json", meta); err != nil {
		return err
	}
	fmt.Printf("  zoom %d-%d · %d new tiles, %d already present\n", minZoom, maxZoom, total, skipped)
	return nil
}

// detectZooms returns the lowest and highest zoom level present on disk.
func detectZooms(dir string) (minZoom, maxZoom int, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, false
	}
	minZoom, maxZoom = 99, -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		z, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if z < minZoom {
			minZoom = z
		}
		if z > maxZoom {
			maxZoom = z
		}
	}
	return minZoom, maxZoom, maxZoom >= 0
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
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "mapfetch: %v\n", err)
	os.Exit(1)
}
