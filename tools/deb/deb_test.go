package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readAr splits an ar archive into its members.
func readAr(t *testing.T, path string) map[string][]byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		t.Fatal("not an ar archive")
	}

	members := map[string][]byte{}
	rest := data[len("!<arch>\n"):]
	for len(rest) >= 60 {
		hdr := rest[:60]
		rest = rest[60:]
		name := strings.TrimSpace(string(hdr[0:16]))
		sizeStr := strings.TrimSpace(string(hdr[48:58]))
		var size int
		for _, c := range sizeStr {
			size = size*10 + int(c-'0')
		}
		if size > len(rest) {
			t.Fatalf("member %q claims %d bytes, %d left", name, size, len(rest))
		}
		members[name] = rest[:size]
		rest = rest[size:]
		if size%2 == 1 && len(rest) > 0 {
			rest = rest[1:] // padding
		}
	}
	return members
}

func readTarGz(t *testing.T, data []byte) map[string]struct {
	mode int64
	body []byte
} {
	t.Helper()

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)

	out := map[string]struct {
		mode int64
		body []byte
	}{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, _ := io.ReadAll(tr)
		out[strings.TrimPrefix(hdr.Name, "./")] = struct {
			mode int64
			body []byte
		}{hdr.Mode, body}
	}
	return out
}

// fixture lays out a miniature source tree and builds a package from it.
func fixture(t *testing.T) (debPath string, t2 *testing.T) {
	t.Helper()
	root := t.TempDir()

	write := func(rel string, data string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	bin := write("dayzctl", "#!/bin/sh\necho dayzctl\n")
	unit := write("dayzctl-web.service", "[Unit]\n")
	config := write("config.yaml", "web:\n  port: 8081\n")
	license := write("LICENSE", "MIT\n")
	write("maps/nasdara/meta.json", `{"id":"nasdara","size":16384,"format":"webp"}`)
	write("maps/nasdara/3/0/0.webp", "tile")
	write("scripts/postinst", "#!/bin/sh\nexit 0\n")

	out := filepath.Join(root, "out")
	path, _, err := build(Options{
		Binary:     bin,
		Maps:       filepath.Join(root, "maps"),
		Unit:       unit,
		Config:     config,
		Scripts:    filepath.Join(root, "scripts"),
		License:    license,
		Out:        out,
		Version:    "1.2.3",
		Arch:       "amd64",
		Package:    "dayzctl",
		Maintainer: "test <test@example.com>",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return path, t
}

func TestPackageLayout(t *testing.T) {
	path, _ := fixture(t)

	if filepath.Base(path) != "dayzctl_1.2.3_amd64.deb" {
		t.Errorf("file name = %q", filepath.Base(path))
	}

	members := readAr(t, path)
	if got := string(members["debian-binary"]); got != "2.0\n" {
		t.Errorf("debian-binary = %q, want 2.0", got)
	}
	if members["control.tar.gz"] == nil || members["data.tar.gz"] == nil {
		t.Fatal("missing control or data archive")
	}

	control := readTarGz(t, members["control.tar.gz"])
	for _, want := range []string{"control", "md5sums", "conffiles", "postinst"} {
		if _, ok := control[want]; !ok {
			t.Errorf("control archive is missing %s", want)
		}
	}
	if got := control["postinst"].mode; got != 0o755 {
		t.Errorf("postinst mode = %o, want 755", got)
	}

	ctrl := string(control["control"].body)
	for _, want := range []string{"Package: dayzctl", "Version: 1.2.3", "Architecture: amd64", "Installed-Size:"} {
		if !strings.Contains(ctrl, want) {
			t.Errorf("control file is missing %q", want)
		}
	}
	if got := strings.TrimSpace(string(control["conffiles"].body)); got != "/etc/dayzctl/config.yaml" {
		t.Errorf("conffiles = %q", got)
	}

	data := readTarGz(t, members["data.tar.gz"])
	expected := map[string]int64{
		"usr/bin/dayzctl":                           0o755,
		"usr/share/dayzctl/maps/nasdara/meta.json":  0o644,
		"usr/share/dayzctl/maps/nasdara/3/0/0.webp": 0o644,
		"lib/systemd/system/dayzctl-web.service":    0o644,
		"etc/dayzctl/config.yaml":                   0o644,
		"usr/share/doc/dayzctl/copyright":           0o644,
	}
	for name, mode := range expected {
		got, ok := data[name]
		if !ok {
			t.Errorf("data archive is missing %s", name)
			continue
		}
		if got.mode != mode {
			t.Errorf("%s mode = %o, want %o", name, got.mode, mode)
		}
	}
	if _, ok := data["usr/"]; !ok {
		t.Error("directory entries are missing")
	}
}

func TestMd5sumsMatchTheData(t *testing.T) {
	path, _ := fixture(t)
	members := readAr(t, path)

	control := readTarGz(t, members["control.tar.gz"])
	data := readTarGz(t, members["data.tar.gz"])

	for _, line := range strings.Split(strings.TrimSpace(string(control["md5sums"].body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("bad md5sums line %q", line)
		}
		entry, ok := data[fields[1]]
		if !ok {
			t.Errorf("md5sums mentions %q, which is not in the data archive", fields[1])
			continue
		}
		if got := md5Hex(entry.body); got != fields[0] {
			t.Errorf("%s: md5 = %s, want %s", fields[1], got, fields[0])
		}
	}
}

func TestBuildRejectsMissingInputs(t *testing.T) {
	_, _, err := build(Options{
		Binary:  filepath.Join(t.TempDir(), "nope"),
		Maps:    t.TempDir(),
		Unit:    filepath.Join(t.TempDir(), "nope.service"),
		Config:  filepath.Join(t.TempDir(), "nope.yaml"),
		Out:     t.TempDir(),
		Version: "1",
		Arch:    "amd64",
	})
	if err == nil {
		t.Fatal("expected an error for missing files")
	}
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}
