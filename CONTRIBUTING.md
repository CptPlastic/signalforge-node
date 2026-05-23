# Contributing To SignalForge Node

SignalForge Node is the public buildable node stack for community scanner hubs.

## How To Contribute

- Open an issue for bugs, setup problems, docs gaps, and feature ideas.
- Open a pull request for focused fixes.
- For larger federation or data-model changes, start with an issue or discussion first.

## Pull Request Expectations

- Keep changes focused and easy to review.
- Explain what changed and why.
- Include screenshots for visible UI changes.
- Run relevant checks before opening the PR.
- Do not commit secrets, private API keys, source upload keys, local deployment files, database dumps, or call audio.
- Preserve the AGPL-3.0-or-later license notices and do not add incompatible license terms.
- Be extra careful with auth, upload keys, federation trust, database migrations, and anything that changes official or verified status.
- Destructive migrations, public deployment defaults, and trust-model changes require maintainer review even if CI passes.

Community pull requests should target `dev`. Maintainer release promotions from `dev` to `main` use the release PR template and titles like `SignalForge Node YYYY.MM.DD`.

## Mirrored Source Changes

Most runtime source paths in this repository are mirrored from `p7-scanner`, including `client/`, `server/`, `docker/`, `docs/`, `tools/`, compose files, and the main build files. Community PRs should still start here against `dev`.

When a PR changes mirrored paths, automation opens or updates a matching draft review PR in `p7-scanner`. Maintainers review and merge the source-of-truth PR there first; after that, the normal sync brings the accepted change back to `signalforge-node/dev`.

This keeps `dev` from accepting mirrored code that was not approved upstream while still giving contributors one public place to open PRs.

## Local Checks

```bash
cd server && go test ./...
cd client && npm ci && npm run build
docker-compose config
```

## Trust Model

Open source does not mean automatic trust. Anyone can run a node, but verified and official status require maintainer approval through SignalForge.

## Contributor License

By submitting a pull request, you agree that your contribution is provided under the same AGPL-3.0-or-later license as this repository.
