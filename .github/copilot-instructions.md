# dayzctl

Go server-management CLI with an embedded web UI (`web/log-analyzer`, plain
JS/CSS/HTML, `//go:embed`ed).

## Lint gate — mandatory after every code change

A change to `.go`, `.js`, `.css` or `.html` is not finished until this has run
and its output has been read:

```bash
gofmt -l internal cmd tools test && go vet ./... && go build ./... && go test ./... -count=1
```

Plus:

- `node --check` on every `web/log-analyzer/js/*.js` (copy to `.mjs` first).
- `make build-local` after editing anything under `web/log-analyzer/` — the
  assets are embedded, so the running binary is stale until rebuilt.
- `make lint` (golangci-lint) is best effort: the installed binary panics on
  this toolchain.

Never report a task as done, fixed, or green while any step is red, and never
weaken a test assertion to get there. Full procedure:
`.github/skills/lint-before-done/SKILL.md`; it is also enforced by
`.github/hooks/lint-gate.json` (fast gate after each edit, full gate at Stop).

## Repository conventions

- errcheck style: `defer func() { _ = f.Close() }()`, `_ = src.Close()`,
  `_, _ = fmt.Fprintf(w, ...)`.
- Tests live next to the code (`*_test.go`); fixtures in `internal/logs/testdata`.
- No new dependencies without a reason; `go.mod` is minimal on purpose.
