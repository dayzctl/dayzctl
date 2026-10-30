// Command deb builds the dayzctl Debian package.
//
// A .deb is an ar archive holding three members: debian-binary,
// control.tar.gz and data.tar.gz. Building it here keeps packaging reproducible
// on any machine — dpkg-deb is not required, so the package can be produced on
// macOS as well as Linux.
//
//	go build -tags nomaps -o build/dayzctl-linux-amd64 ./cmd/dayzctl
//	go run ./tools/deb -bin build/dayzctl-linux-amd64 -out build
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Package layout.
const (
	binPath    = "usr/bin/dayzctl"
	mapsPath   = "usr/share/dayzctl/maps"
	docPath    = "usr/share/doc/dayzctl"
	unitPath   = "lib/systemd/system/dayzctl-web.service"
	configPath = "etc/dayzctl/config.yaml"
)

func main() {
	var (
		bin     = flag.String("bin", "build/dayzctl-linux-amd64", "linux binary to package")
		maps    = flag.String("maps", "web/log-analyzer/maps", "map data directory to ship")
		unit    = flag.String("unit", "packaging/dayzctl-web.service", "systemd unit file")
		config  = flag.String("config", "packaging/config.yaml", "default configuration (installed as a conffile)")
		scripts = flag.String("scripts", "packaging/scripts", "directory with postinst/prerm/postrm")
		license = flag.String("license", "LICENSE", "file installed as /usr/share/doc/dayzctl/copyright")
		out     = flag.String("out", "build", "output directory")
		ver     = flag.String("version", "", "package version (defaults to internal/version/version.txt)")
		arch    = flag.String("arch", "amd64", "package architecture")
		pkgName = flag.String("package", "dayzctl", "package name")
		maint   = flag.String("maintainer", "dayzctl maintainers <maintainers@dayzctl.invalid>", "Maintainer field")
	)
	flag.Parse()

	version := *ver
	if version == "" {
		version = readVersion()
	}

	opt := Options{
		Binary:     *bin,
		Maps:       *maps,
		Unit:       *unit,
		Config:     *config,
		Scripts:    *scripts,
		License:    *license,
		Out:        *out,
		Version:    version,
		Arch:       *arch,
		Package:    *pkgName,
		Maintainer: *maint,
	}

	outFile, size, err := build(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deb: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%.1f MB)\n", outFile, float64(size)/(1<<20))
}

// Options describes one package build.
type Options struct {
	Binary     string
	Maps       string
	Unit       string
	Config     string
	Scripts    string
	License    string
	Out        string
	Version    string
	Arch       string
	Package    string
	Maintainer string
}

func readVersion() string {
	data, err := os.ReadFile("internal/version/version.txt")
	if err != nil {
		return "0.0.0"
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "0.0.0"
	}
	return v
}

// entry is one file or directory in the package.
type entry struct {
	name string // path inside the package, no leading "./"
	mode fs.FileMode
	dir  bool
	src  string // source file on disk (empty for directories)
}

func build(opt Options) (string, int64, error) {
	entries, err := collect(opt)
	if err != nil {
		return "", 0, err
	}

	// The data archive is written first: its file checksums go into the control
	// archive, and the ar header needs every member's size up front.
	dataFile, md5sums, installed, err := writeDataArchive(entries)
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(dataFile)

	control, err := controlArchive(opt, md5sums, installed)
	if err != nil {
		return "", 0, err
	}

	if err := os.MkdirAll(opt.Out, 0o755); err != nil {
		return "", 0, err
	}
	outFile := filepath.Join(opt.Out, fmt.Sprintf("%s_%s_%s.deb", opt.Package, opt.Version, opt.Arch))

	if err := writeAr(outFile, []arMember{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "control.tar.gz", data: control},
		{name: "data.tar.gz", file: dataFile},
	}); err != nil {
		return "", 0, err
	}

	info, err := os.Stat(outFile)
	if err != nil {
		return "", 0, err
	}
	return outFile, info.Size(), nil
}

// collect lists everything the package ships, in a stable order.
func collect(opt Options) ([]entry, error) {
	var entries []entry

	add := func(name string, mode fs.FileMode, src string) {
		entries = append(entries, entry{name: name, mode: mode, src: src})
	}

	// Directories first so the tar stream is well-formed.
	dirs := map[string]bool{
		"usr": true, "usr/bin": true, "usr/share": true, "usr/share/dayzctl": true,
		mapsPath: true, docPath: true, "lib": true, "lib/systemd": true,
		"lib/systemd/system": true, "etc": true, "etc/dayzctl": true,
	}

	maps, err := filepath.Abs(opt.Maps)
	if err != nil {
		return nil, err
	}
	walkErr := filepath.WalkDir(maps, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(maps, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		inside := path.Join(mapsPath, filepath.ToSlash(rel))
		if d.IsDir() {
			dirs[inside] = true
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		add(inside, 0o644, p)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("maps: %w", walkErr)
	}

	// Every directory, plus the ones the walk discovered.
	var names []string
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		entries = append(entries, entry{name: d, mode: 0o755, dir: true})
	}

	required := []struct{ path, dst string }{
		{opt.Binary, binPath},
		{opt.Unit, unitPath},
		{opt.Config, configPath},
	}
	if opt.License != "" {
		required = append(required, struct{ path, dst string }{opt.License, path.Join(docPath, "copyright")})
	}
	for _, r := range required {
		if _, err := os.Stat(r.path); err != nil {
			return nil, fmt.Errorf("missing %s: %w", r.path, err)
		}
		mode := fs.FileMode(0o644)
		if r.dst == binPath {
			mode = 0o755
		}
		add(r.dst, mode, r.path)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

// writeDataArchive streams data.tar.gz to a temp file and returns its path, the
// md5sums of every regular file, and the installed size in KiB.
func writeDataArchive(entries []entry) (string, []string, int, error) {
	tmp, err := os.CreateTemp("", "dayzctl-data-*.tar.gz")
	if err != nil {
		return "", nil, 0, err
	}
	defer tmp.Close()

	gz, err := gzip.NewWriterLevel(tmp, gzip.BestSpeed) // tiles are already compressed
	if err != nil {
		return "", nil, 0, err
	}
	tw := tar.NewWriter(gz)

	now := time.Now()
	var md5sums []string
	var installed int64

	for _, e := range entries {
		hdr := &tar.Header{
			Name:    "./" + e.name,
			Mode:    int64(e.mode.Perm()),
			ModTime: now,
			Uid:     0,
			Gid:     0,
			Uname:   "root",
			Gname:   "root",
			Format:  tar.FormatGNU,
		}
		if e.dir {
			hdr.Name += "/"
			hdr.Typeflag = tar.TypeDir
			if err := tw.WriteHeader(hdr); err != nil {
				return "", nil, 0, err
			}
			continue
		}

		src, err := os.Open(e.src)
		if err != nil {
			return "", nil, 0, err
		}
		info, err := src.Stat()
		if err != nil {
			src.Close()
			return "", nil, 0, err
		}

		hdr.Typeflag = tar.TypeReg
		hdr.Size = info.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			src.Close()
			return "", nil, 0, err
		}

		hash := md5.New()
		if _, err := io.Copy(io.MultiWriter(tw, hash), src); err != nil {
			src.Close()
			return "", nil, 0, err
		}
		src.Close()

		md5sums = append(md5sums, hex.EncodeToString(hash.Sum(nil))+"  "+e.name)
		installed += info.Size()
	}

	if err := tw.Close(); err != nil {
		return "", nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return "", nil, 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", nil, 0, err
	}

	return tmp.Name(), md5sums, int(installed / 1024), nil
}

// controlFile is one member of the control archive.
type controlFile struct {
	name string
	mode fs.FileMode
	data []byte
}

// controlArchive builds control.tar.gz: control, md5sums, conffiles and the
// maintainer scripts.
func controlArchive(opt Options, md5sums []string, installed int) ([]byte, error) {
	control := fmt.Sprintf(`Package: %s
Version: %s
Architecture: %s
Maintainer: %s
Installed-Size: %d
Section: admin
Priority: optional
Homepage: https://github.com/dayzctl/dayzctl
Description: DayZ server manager and ADM/RPT log analyzer
 dayzctl installs and configures DayZ dedicated servers, then analyses their
 ADM and RPT logs: engine errors with explanations, players, kills, base
 building, suspicious activity and a map view with bundled map imagery.
 .
 The package ships the server binary plus the map data under
 /usr/share/dayzctl/maps, so the analyzer reads imagery from disk at runtime
 and needs no network access.
`, opt.Package, opt.Version, opt.Arch, opt.Maintainer, installed)

	files := []controlFile{
		{name: "control", mode: 0o644, data: []byte(control)},
		{name: "md5sums", mode: 0o644, data: []byte(strings.Join(md5sums, "\n") + "\n")},
		{name: "conffiles", mode: 0o644, data: []byte("/" + configPath + "\n")},
	}

	for _, script := range []string{"postinst", "prerm", "postrm"} {
		data, err := os.ReadFile(filepath.Join(opt.Scripts, script))
		if err != nil {
			continue
		}
		files = append(files, controlFile{name: script, mode: 0o755, data: data})
	}

	return tarGz(files)
}

// tarGz builds a gzipped tar archive of the given files in memory.
func tarGz(files []controlFile) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	now := time.Now()
	for _, f := range files {
		hdr := &tar.Header{
			Name:     "./" + f.name,
			Mode:     int64(f.mode.Perm()),
			Size:     int64(len(f.data)),
			ModTime:  now,
			Typeflag: tar.TypeReg,
			Uname:    "root",
			Gname:    "root",
			Format:   tar.FormatGNU,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
