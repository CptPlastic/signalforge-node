# SignalForge Node Release

## Release Name

Use: `SignalForge Node YYYY.MM.DD` or `SignalForge Node YYYY.MM.DD Patch N`

## Promotion

- From: `dev`
- To: `main`

## Summary

Describe the operator-facing changes included in this promotion.

## Checks

- [ ] This PR is opened by a maintainer.
- [ ] Source-of-truth mirrored changes were accepted upstream in `p7-scanner` or are already synced from it.
- [ ] CI is passing on `dev`.
- [ ] CodeQL/security alerts are reviewed or intentionally deferred.
- [ ] Public container/update behavior is understood for this release.
- [ ] No secrets, real `.env` files, database dumps, call audio, or local volumes are included.

## Notes For Operators

List upgrade notes, deployment warnings, config changes, or known issues.