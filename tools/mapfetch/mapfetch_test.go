package main

import "testing"

const registry = `JSON.parse('{"chernarusplus":{"v":"1.30","i":0},"nasdara":{"v":"1.30","i":11},` +
	`"chiemsee":{"v":"4.0","i":11},"banov":{"v":"Jun.15","i":0}}')`

func TestVersionFor(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		// Iteration 0 is dropped on the site: "1.30.json", not "1.30-0.json".
		{"chernarusplus", "1.30"},
		{"banov", "Jun.15"},
		{"nasdara", "1.30-11"},
		{"chiemsee", "4.0-11"},
	}
	for _, tc := range cases {
		got, err := versionFor(registry, tc.id)
		if err != nil {
			t.Errorf("%s: %v", tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: version = %q, want %q", tc.id, got, tc.want)
		}
	}

	if _, err := versionFor(registry, "siberia"); err == nil {
		t.Error("expected an error for a map the registry does not list")
	}
}

func TestAllMapIDsKeepsOrderAndDeduplicates(t *testing.T) {
	bundle := registry + registry // the registry can appear twice in the payload

	ids, err := allMapIDs(bundle)
	if err != nil {
		t.Fatalf("allMapIDs: %v", err)
	}

	want := []string{"chernarusplus", "nasdara", "chiemsee", "banov"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestToPlace(t *testing.T) {
	// Dashtabad as published for nasdara (16 km world).
	p, ok := toPlace("city", []float64{-87.3740387, 166.4884949}, []string{"dashtabad"}, 16384, false)
	if !ok {
		t.Fatal("expected a place")
	}
	if p.Name != "Dashtabad" {
		t.Errorf("name = %q, want Dashtabad", p.Name)
	}
	if p.X < 10654 || p.X > 10657 || p.Y < 10791 || p.Y > 10794 {
		t.Errorf("position = %v,%v, want ~10655,10792", p.X, p.Y)
	}

	inverted, _ := toPlace("city", []float64{-87.37, 166.49}, []string{"x"}, 16384, true)
	plain, _ := toPlace("city", []float64{-87.37, 166.49}, []string{"x"}, 16384, false)
	if plain.Y+inverted.Y != 16384 {
		// round1 leaves half a metre of slack
		if plain.Y+inverted.Y < 16383.5 || plain.Y+inverted.Y > 16384.5 {
			t.Errorf("gpsInvertY must mirror the north axis: %v + %v", plain.Y, inverted.Y)
		}
	}
}

func TestDetectZooms(t *testing.T) {
	dir := t.TempDir()
	for _, z := range []int{3, 4} {
		for _, f := range []string{"0", "1"} {
			if err := writeFile(dir+"/"+itoa(z)+"/"+f+"/0.webp", []byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}

	minZoom, maxZoom, ok := detectZooms(dir)
	if !ok || minZoom != 3 || maxZoom != 4 {
		t.Errorf("detectZooms = %d-%d (ok=%v), want 3-4", minZoom, maxZoom, ok)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
