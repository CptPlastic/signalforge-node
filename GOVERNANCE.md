# Governance

SignalForge Node is open for community contribution, but project authority stays with the maintainers.

## Repository Roles

- Public contributors can open issues, fork the repo, and submit pull requests.
- Trusted contributors may help triage issues and review pull requests if invited.
- Maintainers review and merge pull requests.
- Admins control repository settings, security policy, releases, package publishing, verified status, and official status.

## Contribution Flow

1. Open an issue or discussion for large changes.
2. Fork the repo and open a pull request against `dev` for routine work.
3. Keep the pull request focused.
4. CI must pass before merge.
5. Maintainer review is required when more than one maintainer is available.
6. Conversations must be resolved before merge.
7. Maintainers promote `dev` to `main` for stable public source updates.

Small documentation and bug-fix pull requests are welcome without prior discussion.

## Branches

- `main` is the stable public source line for operators and public containers.
- `dev` is the integration branch for active work and contributor pull requests.
- Feature branches should be short-lived and merge through pull requests.
- Direct pushes to protected branches should be avoided except for repository administration.

## Protected Areas

The following changes require extra maintainer review:

- Authentication, sessions, magic links, roles, or admin checks.
- Source upload keys or ingestion authorization.
- Federation trust rules, peer invites, verified status, official status, or hub identity.
- Database migrations that remove, rewrite, or backfill user/operator data.
- Public container names, update manifests, or deployment defaults.
- Changes that expose ports, secrets, logs, call audio, or private metadata.
- License, governance, security policy, or contributor terms.

## License And Fair Source Alignment

SignalForge Node is licensed AGPL-3.0-or-later. The practical intent is simple: people can run and improve community nodes, but modified network services should give their users the corresponding source code.

SignalForge is fair-source aligned in spirit, but it is not licensed under a proprietary Fair Source License. The public node stack stays open source under AGPLv3-or-later. See [FAIR-SOURCE.md](FAIR-SOURCE.md) for the project posture.

AGPL does not make every commercial use impossible, but it does prevent closed proprietary forks from being run as network services without source availability. Operators who want a separate commercial arrangement, promoted listing, verified status, or official status should contact the maintainers first.

## Official And Verified Status

Anyone can run a node. That does not make the node official.

- Listed means a hub is visible in public discovery.
- Verified means maintainers have validated the hub identity and basic operator information.
- Official means maintainers explicitly trust the hub for promoted SignalForge use.

Verified and official status are never granted automatically by code contribution, container deployment, or successful federation.

## Upstream Relationship

Core development may continue in `projectseven-co-ltd/p7-scanner`. Public changes can also originate here in `signalforge-node` and be ported upstream when they belong in the root development line.

Maintainers decide how and when changes move between repositories.
