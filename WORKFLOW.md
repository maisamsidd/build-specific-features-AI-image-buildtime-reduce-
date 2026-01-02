# Project Workflow

This document explains how the project's build and feature workflow works, referencing the code and YAML configuration present in the repository.

**Overview:**
- **Repository role:** The project implements a small feature-oriented builder that reads `builder.yaml`, computes hashes of feature inputs and dependencies, and decides whether to run a feature's build command or skip it when inputs haven't changed.
- **Entry point:** The CLI runner is `cmd/apis/main.go` (invoked as `builder <command> <feature>` in the code).

**Key Files & Directories:**
- **`builder.yaml`**: YAML config listing features, their `inputs` (paths), `command` (build script), and `depends_on` (other features).
- **`cmd/apis/main.go`**: Main builder logic. Loads `builder.yaml`, resolves dependencies, computes hashes, and uses the cache to decide SKIP/BUILD.
- **`internal/config/config.go`**: Go structs used to decode `builder.yaml` into `Feature` and `BuilderConfig`.
- **`internal/hash/hash.go`**: Utilities to hash strings, files, and directories deterministically (used to detect changes in feature inputs).
- **`internal/feature/feature_hash.go`**: Higher-level function that composes command, inputs, and dependency hashes into a single feature hash.
- **`internal/cache/cache.go`**: Simple filesystem cache under `.builder-cache/<feature>/hash` to store previously computed feature hash.
- **`features/`**: Feature directories (each feature should include its build script, e.g. `build.sh`) referenced by `builder.yaml`.
- **`dockerfile/` and `makefile/`**: Top-level directories present in repository (not standard `Dockerfile`/`Makefile` files). They likely contain project-specific packaging scripts—inspect them as needed.

**Runtime flow (what happens when you run the builder):**
1. CLI is invoked (in code: `main()` in `cmd/apis/main.go`) expecting at least a feature name.
2. The program reads and unmarshals `builder.yaml` into a `BuilderConfig`.
3. For the requested feature:
   - Names are attached to each `Feature` entry so the code can reference them by name.
   - Dependencies listed in `depends_on` are looked up in the same YAML.
4. The builder computes hashes:
   - For dependencies: `HashDir()` is used on dependency input dirs to produce a dependency hash.
   - For the feature itself: `ComputeFeatureHash()` (in `cmd/apis/main.go` or via `internal/feature`) composes the feature's `command`, hashes of its `inputs` (directories), and dependency hashes into a single fingerprint.
5. The builder reads the stored cached value using `internal/cache.ReadFeatureHash(<feature>)`.
   - If cached hash matches new hash => prints `SKIP build for <feature>` and exits.
   - If different or missing => prints `BUILD feature <feature>` and (TODO in code) would execute the feature's `command` (for now, it simply updates the cache with `WriteFeatureHash`).

**Hashing details:**
- `internal/hash.HashDir` walks the directory, sorts file paths, hashes each file's contents, and combines them deterministically.
- `HashString` and `HashFile` provide helper primitives.
- `internal/feature` builds a deterministic final string (command + input hashes + sorted dependency entries) and hashes that to produce the feature fingerprint.

**Cache format & location:**
- Cache file path: `.builder-cache/<feature>/hash` (see `internal/cache.FeatureHashPath`).
- Cache stores only the last computed feature hash as plain text.

**YAML-driven behavior:**
- `builder.yaml` controls ordering and inputs. Example snippet:

```yaml
features:
  feature2:
    inputs:
      - /home/maisam/go/platform-api/features/feature2
    command: ./build.sh
    depends_on:
      - feature1
```

This means `feature2`'s build uses files under the specified `inputs` path and will incorporate `feature1`'s output/input hash when computing whether `feature2` needs rebuilding.

**How to run (development):**
- Build/run locally from project root:

```bash
go run ./cmd/apis/main.go build <feature-name>
```

- Replace `<feature-name>` with a key from `builder.yaml` (e.g., `feature2`).

**Extending / Next steps:**
- Implement execution of the feature `command` where the code currently has the `TODO` (in `cmd/apis/main.go`) so that `BUILD` actually runs the build script.
- Add better error handling and support for multiple `inputs` per dependency, if needed.
- Provide relative path normalization and validation for `inputs` so the YAML can use relative paths safely.
- Consider parallel builds of independent features (respecting `depends_on`) for speed.

**Troubleshooting notes:**
- If a feature is unexpectedly skipped, check the contents of `.builder-cache/<feature>/hash` and recompute hashes manually with small test runs.
- Ensure `features/*` directories and `build.sh` exist and are executable when referenced by `builder.yaml`.

---
If you want, I can:
- implement running the `command` when a build is required,
- add a `--force` flag to bypass cache, or
- generate a diagram of the dependency graph from `builder.yaml`.
