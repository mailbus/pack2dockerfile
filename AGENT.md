# AGENT.md

Guidance for AI agents and contributors working in this repository.

## What this project is

`pack2dockerfile` (CLI: `p2df`) turns a built Cloud Native Buildpacks (CNB) image back into a
human-readable, rebuildable Dockerfile. **The Dockerfile is the artifact, not the source of truth.**

Correctness is defined by behavior, not by textual similarity to the buildpack build:

1. read the built image (metadata, labels, `/layers`, SBOM);
2. render a multi-stage Dockerfile;
3. rebuild it;
4. require four checks to pass against the original image: **process, smoke, user, env**.

## Repository map

| Path | Purpose |
| --- | --- |
| `cmd/p2df/` | cobra CLI; reads the image, resolves the runtime version, renders the Dockerfile |
| `internal/imageinfo/` | `pack inspect-image` + CNB labels → image model; language detection; SBOM version lookup |
| `internal/dockerfile/` | pure rendering: language runtime table, env parsing/translation, Dockerfile template |
| `internal/verify/` | behavior-equivalence harness; `verify_e2e_test.go` is `//go:build e2e` |
| `fixtures/` | minimal apps per language, each with `GET /health` |
| `docs/` | internal-only (separate git repo, ignored here): spec, tickets, experiment records |

## Commands

```bash
go test ./...                       # unit tests (no Docker/pack needed)
go build -o p2df ./cmd/p2df

p2df <image> --builder paketobuildpacks/builder-jammy-full > Dockerfile

# End-to-end (needs Docker + pack + the original image built locally)
P2DF_TEST_IMAGE=<image> go test -tags e2e ./internal/verify -run 'TestGenerated(Node|Python|Go|Java)Dockerfile' -v
```

## Rules for changes

- **Understand the runtime contract before editing.** Inspect the original image:
  `pack inspect-image <image> --output json`, `docker inspect`, the `/layers/**/env[.launch]` files,
  `pack sbom download`, and the runtime PID1 env (`/proc/1/environ`).
- **Language differences belong in one place.** Add or adjust the `runtimeFor` case in
  `internal/dockerfile/dockerfile.go`; keep the run stage, header, and verifier shared.
- **A new runtime version lookup** goes in `internal/imageinfo/sbom.go` and must be wired into both
  `cmd/p2df/main.go` and `internal/verify/verify_e2e_test.go`.
- **Pass-line changes require a spec revision.** Adding a variable to the noise whitelist is a
  revision of the acceptance criteria; record the reason in the internal spec and experiment record.
  Do not whitelist a variable merely to make a check green.
- **One runnable check per behavior change.** Unit-test rendering and env parsing; reserve
  `//go:build e2e` for the four-check closed loop.
- **Do not copy buildpack-internal `/layers` paths into a generated Dockerfile.** Translate them to
  the equivalent location in the generated image, or exclude them; document why.
- **Commits:** Chinese messages, `feat(scope): ...` / `fix(scope): ...`, with the project's
  `Co-Authored-By` trailer. Do not commit `docs/` to the public repository.

## Known limitations

See `README.md`. In short: only the default process is restored; `exec.d` dynamic variables are not
reproduced; the run stage reuses the original CNB run image; Java support covers the Paketo
executable-jar layout only.
