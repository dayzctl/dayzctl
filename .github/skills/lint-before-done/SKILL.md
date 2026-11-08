---
name: lint-before-done
description: 'Run the dayzctl lint/format/vet/test gate after ANY code edit, before calling a task done. Covers gofmt, go vet, go build, go test ./..., node --check for the embedded web assets, the errcheck conventions used in this repo, and the broken local golangci-lint. Use when editing .go/.js/.css/.html files, fixing a build or test failure, finishing a task, or when the user says lint, format, vet, test, verify, green, check, or done.'
argument-hint: 'Optional package or path to focus on (the gate still runs repo-wide)'
---

# Lint before done

## Enforcement

This skill is the procedure; `.github/hooks/lint-gate.json` is what makes it
non-optional:

- `PostToolUse` runs `lint-gate.sh --fast` after any edit to `.go/.js/.css/.html`
  — gofmt + vet + build + `node --check` (~3 s) — and **blocks the tool result**
  with the output when it fails.
- `Stop` runs `lint-gate.sh --full`, which adds `go test ./... -count=1`, and
  **blocks the end of the turn** when the gate is red.

So a red gate cannot be ignored or forgotten: the edit is rejected and the
session cannot finish until it is fixed. If the hook ever fails to fire, run the
gate below by hand.

## Hard rule

A change is **not** done, fixed, or "green" until the gate below has run in the
same turn and you have read its output. Never claim success from reasoning
alone, and never report a partial gate ("only the touched package") as passing.

## Gate (in this order)

1. **Format check** — must print nothing:
   ```bash
   gofmt -l internal cmd tools test
   ```
   Fix with `gofmt -w <files>`, then re-run until empty.

2. **Vet**:
   ```bash
   go vet ./...
   ```

3. **Build**:
   ```bash
   go build ./...
   ```

4. **Tests — whole repo**, not just the package you touched (a shared-code edit
   once broke a handler assertion that only another package's tests caught):
   ```bash
   go test ./... -count=1
   ```

5. **Browser JS syntax** (the log analyzer is plain JS, no build step):
   ```bash
   for f in web/log-analyzer/js/*.js; do
     cp "$f" "/tmp/$(basename "$f").mjs"
     node --check "/tmp/$(basename "$f").mjs" || echo "FAIL $f"
   done
   ```

6. **Rebuild after asset edits** — `web/log-analyzer/**` is `//go:embed`ed, so
   a JS/CSS/HTML change is invisible to the running binary until:
   ```bash
   make build-local
   ```

7. **golangci-lint (best effort only)**:
   ```bash
   make lint
   ```
   The binary installed on this machine panics (built with go1.26, module needs
   go1.27). Do not block on it; if it does run, fix what it reports.

A combined one-liner for the fast part:

```bash
gofmt -l internal cmd tools test && go vet ./... && go build ./... && go test ./... -count=1
```

## errcheck conventions used in this repo

Match the existing style instead of sprinkling `//nolint`:

- deferred close: `defer func() { _ = f.Close() }()`
- close on an error path: `_ = src.Close()`
- writes whose error is irrelevant: `_, _ = fmt.Fprintf(w, ...)`
- close error that must win: named return + `if cerr := out.Close(); cerr != nil && err == nil { err = cerr }`

## When the gate fails

1. Read the first failure, not the tail.
2. Fix the cause; do not delete or weaken a test assertion to make it pass.
3. Re-run the **whole** gate, not only the command that failed.
4. If a failure is genuinely unrelated to the change, say so explicitly with the
   error text — never silently skip it.

## Report template

End the task with the actual result of each step, for example:

```
gofmt: clean · vet: clean · build: clean · test ./...: ok (all packages) · node --check: ok
```

## Do not

- Do not run `git commit`, `git add` or `git push` as part of this gate; the
  user decides when to commit.
- Do not run `go test ./internal/...` alone when code outside that tree changed.
- Do not treat `go test` output filtered through `grep -v '^ok'` as proof if you
  also filtered `FAIL`.
