// Command mapfetch downloads map imagery and place names from the community map
// at dayz.xam.nu and writes them into this repository.
//
// The web UI embeds whatever lives under web/log-analyzer/maps, so running this
// command is the only moment dayzctl talks to the map host: the server itself
// never does. Re-run it to refresh a map after the map's author publishes an
// update.
//
//	go run ./tools/mapfetch -map nasdara -zooms 3,4
//	go run ./tools/mapfetch -all                    # reuses each map's zoom levels
//	go run ./tools/mapfetch -map livonia -styles topographic
//
// Tiles are cached in the output directory, so an interrupted run resumes and a
// second run only fetches what is missing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		styles  = flag.String("styles", "satellite,topographic", "comma-separated imagery styles (satellite, topographic)")
		out     = flag.String("out", "web/log-analyzer/maps", "output directory")
		force   = flag.Bool("force", false, "re-download tiles that already exist")
		workers = flag.Int("workers", 2, "parallel requests inside one map")
		par     = flag.Int("parallel", 3, "maps fetched concurrently")
		rename  = flag.Bool("fixnames", false, "rewrite meta.json display names from the local map ids (no network)")
	)
	flag.Parse()

	if *rename {
		fail(fixNames(*out))
		return
	}

	levels, err := parseZooms(*zooms)
	if err != nil {
		fail(err)
	}
	// A second run over an existing tree must refresh exactly the detail that
	// is already there, so an unspecified -zooms follows each map on disk.
	zoomsGiven := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "zooms" {
			zoomsGiven = true
		}
	})

	sheets, err := parseStyles(*styles)
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
		opts := fetchOptions{zooms: levels, styles: sheets, out: *out, force: *force, zoomsGiven: zoomsGiven}
		fail(fetchAll(src, ids, opts, *par))
		return
	}

	if *mapID == "" {
		fail(fmt.Errorf("nothing to do: pass -map <id>, -all, or -list"))
	}
	fail(fetchOne(src, *mapID, fetchOptions{zooms: levels, styles: sheets, out: *out, force: *force, zoomsGiven: zoomsGiven}))
}

// fetchOptions is what every map in one run shares.
type fetchOptions struct {
	zooms      []int
	styles     []string
	out        string
	force      bool
	zoomsGiven bool // -zooms was passed: do not reuse the levels on disk
}

// fetchAll downloads every published map, skipping the ones already complete.
// Maps are fetched a few at a time: a single stream is latency-bound and a full
// run would otherwise take close to an hour.
func fetchAll(src *source, ids []string, opts fetchOptions, parallel int) error {
	if parallel < 1 {
		parallel = 1
	}
	fmt.Printf("fetching %d maps (zoom %v, styles %s, %d at a time)\n",
		len(ids), opts.zooms, strings.Join(opts.styles, ","), parallel)

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

			if err := fetchOne(src, id, opts); err != nil {
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
	fmt.Printf("\nall %d maps written to %s\n", len(ids), opts.out)
	return nil
}

func fetchOne(src *source, mapID string, opts fetchOptions) error {
	rev, err := src.revision(mapID)
	if err != nil {
		return err
	}

	cfg, err := src.config(mapID, rev)
	if err != nil {
		return err
	}
	meta := cfg.meta(mapID)
	fmt.Printf("  version %s · world %s m · %d place names\n", rev, fmtNum(meta.Size), len(meta.Names))

	dir := filepath.Join(opts.out, mapID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// A map already on disk is refreshed at the detail it already has, so a
	// second run adds the missing style without fetching a new pyramid shape.
	if !opts.zoomsGiven {
		if have := existingZooms(dir); len(have) > 0 {
			opts.zooms = have
		}
	}
	if len(opts.zooms) == 0 {
		return fmt.Errorf("no zoom levels to fetch")
	}

	total, skipped := 0, 0
	shipped := make([]string, 0, len(opts.styles))
	for _, style := range opts.styles {
		// Maps do not all publish every style; ask for the top-left tile before
		// spending a few hundred requests on a pyramid that is not there.
		if !fileExists(filepath.Join(dir, style)) && !src.hasStyle(cfg, style, opts.zooms[0]) {
			fmt.Printf("  no %s imagery published for this map\n", style)
			continue
		}

		wrote, kept, err := styleTarget{
			src:    src,
			cfg:    cfg,
			dir:    dir,
			style:  style,
			format: meta.Format,
			force:  opts.force,
		}.fetchPyramid(opts.zooms)
		if err != nil {
			return err
		}
		total += wrote
		skipped += kept
		shipped = append(shipped, style)
	}
	if len(shipped) == 0 {
		return fmt.Errorf("the map host publishes none of the requested styles")
	}

	// The zoom range reflects what is actually on disk, so a map fetched at
	// several zoom levels over separate runs keeps every level it has.
	minZoom, maxZoom, ok := detectZooms(dir)
	if !ok {
		return fmt.Errorf("no tiles were written")
	}
	meta.MinZoom, meta.MaxZoom = minZoom, maxZoom
	meta.Styles = shipped

	if err := writeMeta(filepath.Join(dir, "meta.json"), meta); err != nil {
		return err
	}
	fmt.Printf("  zoom %d-%d · styles %s · %d new tiles, %d already present\n",
		minZoom, maxZoom, strings.Join(shipped, "+"), total, skipped)
	return nil
}

// styleTarget is where one style's pyramid is written.
type styleTarget struct {
	src    *source
	cfg    *xamConfig
	dir    string
	style  string
	format string
	force  bool
}

// fetchPyramid downloads every tile of the style at the given zoom levels.
// Tiles already on disk are kept unless force is set, so an interrupted run
// resumes where it stopped.
func (t styleTarget) fetchPyramid(zooms []int) (written, kept int, err error) {
	for _, z := range zooms {
		n := 1 << z
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				saved, err := t.fetchTile(z, x, y)
				if err != nil {
					return written, kept, err
				}
				if saved {
					written++
					continue
				}
				kept++
			}
		}
	}
	return written, kept, nil
}

// fetchTile downloads one tile unless it is already on disk. The bool reports
// whether the tile was written.
func (t styleTarget) fetchTile(z, x, y int) (bool, error) {
	path := filepath.Join(t.dir, t.style, strconv.Itoa(z), strconv.Itoa(x), fmt.Sprintf("%d.%s", y, t.format))
	if !t.force && fileExists(path) {
		return false, nil
	}
	data, err := t.src.tile(t.cfg, t.style, z, x, y)
	if err != nil {
		return false, fmt.Errorf("%s tile %d/%d/%d: %w", t.style, z, x, y, err)
	}
	if err := writeFile(path, data); err != nil {
		return false, err
	}
	return true, nil
}

// detectZooms returns the lowest and highest zoom level present on disk. Both
// layouts count: the per-style one (<map>/<style>/<z>/…) and the flat one
// written before styles existed (<map>/<z>/…).
func detectZooms(dir string) (minZoom, maxZoom int, ok bool) {
	levels := existingZooms(dir)
	if len(levels) == 0 {
		return 0, 0, false
	}
	return levels[0], levels[len(levels)-1], true
}

// existingZooms lists every zoom level found under dir, ascending.
func existingZooms(dir string) []int {
	seen := map[int]bool{}
	note := func(name string) {
		if z, err := strconv.Atoi(name); err == nil {
			seen[z] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err == nil {
			note(e.Name())
			continue
		}
		// A style directory holds the zoom levels of that style.
		sub, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, s := range sub {
			if s.IsDir() {
				note(s.Name())
			}
		}
	}
	levels := make([]int, 0, len(seen))
	for z := range seen {
		levels = append(levels, z)
	}
	sort.Ints(levels)
	return levels
}

// fixNames refreshes the display name in every meta.json already on disk, so a
// naming fix does not require re-downloading the tiles.
func fixNames(out string) error {
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}

	updated := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(out, e.Name(), "meta.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var meta metaFile
		if err := json.Unmarshal(raw, &meta); err != nil {
			fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)
			continue
		}
		want := displayName(e.Name())
		if meta.Name == want {
			continue
		}
		meta.Name = want
		if err := writeMeta(path, &meta); err != nil {
			return err
		}
		updated++
	}

	fmt.Printf("updated the name of %d map(s) in %s\n", updated, out)
	return nil
}

// knownStyles are the imagery styles the server can serve. The host publishes
// both for most maps; satellite is the photograph, topographic the road map.
var knownStyles = []string{"satellite", "topographic"}

// parseStyles validates the requested styles, keeping the order given (the
// first one is what the UI opens with after the topographic default).
func parseStyles(raw string) ([]string, error) {
	var picked []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		known := false
		for _, k := range knownStyles {
			if name == k {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown style %q (known: %s)", name, strings.Join(knownStyles, ", "))
		}
		if !seen[name] {
			seen[name] = true
			picked = append(picked, name)
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("no styles given")
	}
	return picked, nil
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
