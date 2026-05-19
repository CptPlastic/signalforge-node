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

## Local Checks

```bash
cd server && go test ./...
cd client && npm ci && npm run build
docker-compose config
```

## Trust Model

Open source does not mean automatic trust. Anyone can run a node, but verified and official status require maintainer approval through SignalForge.
