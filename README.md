# pack2dockerfile (`p2df`)

Turn a built [Cloud Native Buildpacks](https://buildpacks.io/) (CNB) image back into a human-readable, rebuildable Dockerfile.

> Dockerfile is the artifact, not the source of truth.

Buildpacks give you reproducible builds, but the result is locked behind a builder. `p2df` reconstructs the **runtime contract** from the built image — `pack inspect-image` metadata, image labels, `/layers`, and the SBOM — and emits a Dockerfile that rebuilds to a behaviorally equivalent image. No source code or build plan required.

## Status

MVP1 complete. Node.js, Python, Go, and Java are supported end to end. Verified against `paketobuildpacks/builder-jammy-full`; the four fixture apps under [`fixtures/`](fixtures/) pass the full four-check loop.

## Usage

Requirements: Docker, pack (0.40+), and the CNB image available locally.

```bash
go build -o p2df ./cmd/p2df

# Emit a Dockerfile for a locally built Paketo Node image
./p2df node-hello --builder paketobuildpacks/builder-jammy-full > Dockerfile

# Rebuild and run it
docker build -t node-hello-rebuilt -f Dockerfile fixtures/node-hello
docker run --rm -p 8080:8080 node-hello-rebuilt
```

`-o FILE` writes the Dockerfile to a file instead of stdout. `--builder` only annotates the header — the builder is not recorded in the image.

## Supported languages

| Language | Build stage | Notes |
| --- | --- | --- |
| Node.js | `node:<version>` | Node distribution copied from `/usr/local` |
| Python | `python:<version>-bookworm` | bookworm is required for jammy's glibc |
| Go | `golang:<version>-bookworm` | only the compiled binary is copied |
| Java | `maven:3.9-eclipse-temurin-<major>-jammy` | the Paketo run image has no JRE, so the Temurin JDK is copied from the build stage |

## How it works

- Reads image metadata from `pack inspect-image` and the CNB labels. If pack cannot reach a registry, it falls back to the labels alone.
- Resolves the runtime version from the image SBOM (`pack sbom download`), e.g. the `Node Engine` component or `BellSoft Liberica JRE`.
- Reads static runtime env from `/layers/**/env` and `/layers/**/env.launch`.
- Renders a multi-stage Dockerfile:
  - build stage: official runtime image (`node:<version>`, `python:<version>-bookworm`, `golang:<version>-bookworm`, `maven:3.9-eclipse-temurin-<major>-jammy`), then `npm ci`/`npm install`, `pip install`, `go build`, or `mvn package`;
  - run stage: the original CNB run image, runtime user and workdir from the image, application and language runtime copied in, static env, and an exec-form `ENTRYPOINT` for the default process;
  - header comments with the original image digest, buildpack versions, and the full process table.

## Behavioral equivalence

A generated Dockerfile is considered equivalent only when all four checks pass:

1. **process** — the normalized default process command+args;
2. **smoke** — `GET /health` status and body (within 30s);
3. **user** — runtime UID;
4. **env** — PID1 runtime environment, excluding a noise whitelist (infrastructure vars and `exec.d`-derived values).

```bash
# Unit tests
go test ./...

# End-to-end verification (needs Docker + pack + a local CNB image)
P2DF_TEST_IMAGE=node-hello go test -tags e2e ./internal/verify -v
```

## Known limitations

- The build stage is an equivalent construction (`npm install`, `pip install`, `go build`, `mvn package`), not a replay of the buildpack build plan.
- Only the default process is restored; additional processes are listed in comments and can be run with `docker run --entrypoint`.
- `exec.d` dynamic variables (e.g. `MEMORY_AVAILABLE`, Java's `JAVA_TOOL_OPTIONS` and `NMT_LEVEL_*`) are not reproduced.
- Generated images carry no `io.buildpacks.*` labels, SBOM, or rebase capability.
- The run stage reuses the original CNB run image, so part of the equivalence comes from the shared base.
- Java support targets the Paketo executable-jar layout (`java <main-class>` with `CLASSPATH=/workspace`). Spring Boot, Tomcat, Liberty, and dist-zip layouts are not verified yet.

## License

[MIT](LICENSE)
