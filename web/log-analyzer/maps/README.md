# Bundled map data

Everything in this directory is generated ahead of time and embedded into the
`dayzctl` binary with `go:embed`. The web service never downloads map data while
it runs, so the Map tab works without internet access.

Layout:

```
<map>/meta.json          world size, zoom range, attribution, place names
<map>/<z>/<x>/<y>.webp   one satellite tile (256 px, 2^z tiles per side)
```

Regenerate or add a map with the generator:

```sh
go run ./tools/mapfetch -map nasdara -zooms 3,4
go run ./tools/mapfetch -map chernarusplus -zooms 3,4
# or: make maps MAP=nasdara ZOOMS=3,4
```

It resolves the map's current version, converts the published place names to
DayZ world metres and writes the tiles, skipping anything already present
(`-force` re-downloads). Commit the result and rebuild — `go:embed` picks the
files up automatically.

Zoom levels trade size for detail: for a 16 384 m map, `-zooms 3,4` is 320 tiles
(~5.6 MB) and gives 4 m per pixel, which is enough to see individual settlements
under player markers. Add zoom 5 only if you really need more detail; it costs
roughly 20 MB per map.

Tile imagery and place names come from the community map at
<https://dayz.xam.nu> and remain © their authors. They are fetched here, at
generation time, and are not redistributed by dayzctl itself.
