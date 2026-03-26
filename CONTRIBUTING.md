# Contributing to KubeSolv

## Branch Strategy

```
main (protected — no direct pushes)
 ├── feat/<feature-name>     ← new features
 ├── fix/<bug-description>   ← bug fixes
 ├── chore/<task>             ← CI, docs, deps
 └── release/<version>       ← release candidates
```

## Rules

1. **Never push directly to `main`** — all changes go through PRs
2. **Every PR must pass** Lint and Test CI checks before merge
3. **Every PR needs 1 approval** before merge
4. **Branch naming**: `feat/`, `fix/`, `chore/`, `release/`
5. **Commit messages**: use [Conventional Commits](https://www.conventionalcommits.org/)
   - `feat: add multi-user telegram support`
   - `fix: correct OOM container detection`
   - `chore: update CI pipeline`

## Development Flow

```bash
# 1. Create a feature branch
git checkout main && git pull
git checkout -b feat/my-feature

# 2. Make changes, test locally
go build ./...
go vet ./...
go test ./... -race

# 3. Commit and push
git add -A
git commit -m "feat: description of change"
git push origin feat/my-feature

# 4. Open PR on GitHub — CI runs automatically
# 5. Get review → merge → branch auto-deleted
```

## Code Standards

- Go 1.25+
- `go vet` must pass
- All tests must pass with `-race`
- New features need tests
- Apache 2.0 license headers on new files
