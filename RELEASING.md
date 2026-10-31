# Releasing

A `v*` tag runs [.github/workflows/release.yml](.github/workflows/release.yml):

1. `image`: builds `linux/amd64` and `linux/arm64` from the `Dockerfile` and pushes
   `ghcr.io/cobanov/runyard:<version>` and `:latest` (latest only for plain `vX.Y.Z` tags).
2. `pypi`: writes the tag's version into `sdk/python/runyard/__init__.py` (pyproject reads it from there, so
   the wheel, the sdist and `runyard --version` agree), builds both and publishes `runyard` to PyPI.
3. `release`: once both succeed, a GitHub release for the tag with generated notes.

[.github/workflows/ci.yml](.github/workflows/ci.yml) runs on every push and pull request: the web build,
`go vet` and `go test` against a Postgres 17 service, then pytest.

## One-time setup

- **Actions**: Settings → Actions → General: allow actions (on by default for a public repo). The workflows
  set their own permissions, so the default read-only token setting is fine.
- **Environment `pypi`**: Settings → Environments → New environment, name `pypi`. Optionally add yourself as a
  required reviewer so every PyPI upload waits for a click, and limit it to tags `v*`.
- **PyPI trusted publisher**: the project does not exist yet, so add a *pending* publisher at
  https://pypi.org/manage/account/publishing/ :
  PyPI project name `runyard`, owner `cobanov`, repository `runyard`, workflow `release.yml`,
  environment `pypi`. No API token is stored anywhere; the first successful run creates the project.
- **ghcr package**: after the first release run, `runyard` appears under your GitHub profile → Packages,
  private. Package settings → Change visibility → Public, so `docker pull` needs no login. The image carries
  the `org.opencontainers.image.source` label, so the package is already linked to this repo.

## Release

```sh
git checkout main && git pull        # CI green on this commit
git tag v0.2.0
git push origin v0.2.0
```

Nothing is bumped by hand: the tag is the version. `__version__` in the repo stays at the dev version.
Use plain `vX.Y.Z` tags; pre-release tags have not been tried (the image tag and the PyPI version would be
spelled differently).

If a job fails, fix it and use "Re-run failed jobs" on the same run. PyPI never accepts the same version
twice, so if `pypi` already succeeded, do not re-run it; a broken release on PyPI is yanked and fixed with
the next patch version.

After the first release, the README's install line can become `pip install "runyard[images,system]"`, and
`docker-compose.yml` can use `image: ghcr.io/cobanov/runyard:latest` instead of building.
The upstream transformers and accelerate PRs wait for that first release: [upstream/README.md](upstream/README.md).
