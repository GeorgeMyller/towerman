# Contributing

## Build and test

```sh
go build ./...
go vet ./...
go test ./...
cd apple && tools/test.sh          # Swift policy tests, no Xcode project needed
```

The Apple apps need `apple/Config/Local.xcconfig`: see [docs/en/apple-signing.md](docs/en/apple-signing.md). Do not commit it.

CI runs the Go tests on Linux, macOS and Windows, plus cross builds.

## Conventions

- Code, comments and commit messages are in Portuguese. Issues and pull requests can be in English or Portuguese.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`. Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `ci`, `chore`. A breaking change takes `!`.
- Branches: `type/short-description`.
- Versions follow [SemVer](https://semver.org/), with `-rc.N` for release candidates. Never reuse a published version or tag.
- Change `apple/project.yml`, never the generated `Towerman.xcodeproj` by hand. Run `xcodegen generate` and commit both.

## Release

1. Update `Versao` in `internal/config/config.go` and `MARKETING_VERSION` in `apple/project.yml`.
2. Tag: `git tag v<version> && git push origin v<version>`.
3. Run the `release` workflow: `gh workflow run release -f tag=v<version>`. It builds the binaries, the DMG and `SHA256SUMS`, and publishes the release.

## Never commit

- Team IDs, bundle IDs or device UDIDs of your own (they go in `apple/Config/Local.xcconfig` and `~/.towerman/renovar.conf`).
- Relay URLs or tokens, APNs keys.
