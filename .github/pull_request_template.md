## Summary

<!-- One or two sentences describing what this PR does and why. -->

## Changes

<!-- Bullet list of concrete changes. -->

- 

## Affected modules

<!-- Check all that apply -->

- [ ] `cmd/mvplite` — MVP Lite binary
- [ ] `cmd/genpic` — Full platform binary
- [ ] `internal/api` — HTTP handlers / DTOs
- [ ] `internal/provider/openai` — OpenAI adapter
- [ ] `internal/provider/gemini` — Gemini adapter
- [ ] `internal/provider/wan` — Wan adapter
- [ ] `pkg/*` — Shared packages (requires second reviewer)
- [ ] `contracts/providers.yaml` — Model contract table
- [ ] `openapi.yaml` — External API contract
- [ ] `web/` — Frontend assets
- [ ] `docs/` — Documentation / ADRs

## Breaking changes

<!-- Does this change the openapi.yaml contract, a pkg/ interface, or the DB schema? -->

- [ ] No breaking changes
- [ ] Breaking change in openapi.yaml — updated `CHANGELOG.md`
- [ ] Breaking `pkg/*` interface change — ADR filed: `docs/decisions/ADR-NNN-*.md`
- [ ] DB migration required — migration file added

## Test coverage

<!-- Describe what tests were added or why tests were not needed. -->

- [ ] Unit tests added / updated
- [ ] New provider has a `Fake` implementation in `pkg/provider`
- [ ] No tests needed (explain why)

## Rollback plan

<!-- How would you revert this if something goes wrong in production? -->

## Checklist

- [ ] `go vet ./...` passes locally
- [ ] `gofmt -s -l .` shows no unformatted files
- [ ] All exported symbols have godoc comments
- [ ] New config variables documented in `docs/runbook.md` and `config.example.yaml`
- [ ] This PR does NOT include hard-coded upstream credentials
