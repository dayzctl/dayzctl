let the server read maps f# Bundled map data

Everything in this directory is generated ahead of time and embedded into the
`dayzctl` binary with `go:embed`. The web service never downloads map data while
it runs, so the Map tab works without internet access.

46 maps ship here (33 MB): the vanilla terrains plus the community maps
published on the map host — Chernarus, Livonia, Sakhal, Namalsk, Deer Isle,
Esseker, Banov, Namalsk, Pripyat, Takistan Plus and the rest. Each map carries
its own world size and place names, so an instance only needs
`map: <id>` in the config.

Layout:

```
<map>/meta.json          world size, zoom range, attribution, place names
<map>/<z>/<x>/<y>.webp   one tile (256 px, 2^z tiles per side)
```

Tiles are WebP where the map host serves WebP and JPEG where it serves JPEG;
`meta.json` records which, and the server sends the matching content type.

## Regenerating

```sh
go run ./tools/mapfetch -list                     # ids the map host publishes
go run ./tools/mapfetch -all -zooms 3             # every map, 64 tiles each
go run ./tools/mapfetch -map nasdara -zooms 3,4   # one map, extra detail
# or: make maps-all ZOOMS=3   /   make maps MAP=chernarusplus ZOOMS=3,4
```

The generator resolves each map's current version, converts the published place
names to DayZ world metres and writes the tiles. It skips anything already
present, so an interrupted run resumes; `-force` re-downloads. Removing a map is
just deleting its directory — `meta.json` is what the server offers.

## Zoom levels

One zoom level costs `4^z` tiles. For a 16 km map:

| zooms | tiles | size | detail |
| --- | --- | --- | --- |
| `3` | 64 | ~1 MB | 8 m per pixel — fine for a full-map view |
| `3,4` | 320 | ~5 MB | 4 m per pixel — sharp when zoomed in |
| `3,4,5` | 1344 | ~22 MB | only worth it on very large displays |

The UI picks the zoom that matches the canvas size and clamps it to the range in
`meta.json`, so a map shipped at zoom 3 alone still renders correctly.

## Provenance

Tile imagery and place names come from the community map at
<https://dayz.xam.nu> and remain © their authors. They are fetched here, at
generation time, and are not redistributed by dayzctl itself.
