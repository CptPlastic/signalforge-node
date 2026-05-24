# SignalForge Release Channels

SignalForge has three repositories in the working set. They do not all publish the same thing.

## Source of Truth

`p7-scanner` is the private/source-of-truth development repository. Product code, release workflows, image publishing, and generated public artifacts originate here.

## Public Hub Source

`signalforge-node` is the public buildable hub source mirror. It is where operators clone the hub stack, review code, and open community PRs. It should not publish official binary releases. Accepted changes flow back from `p7-scanner` to `signalforge-node/dev` through the sync workflow.

## Public Site and Manifests

`signalforge.org` is the public static site, directory feed, and hub image update manifest host. It should not build binaries. It may display download links that point to releases published from `p7-scanner`.

## Current Release Channels

| Channel | Repository | Trigger | Output | Public Consumer |
| --- | --- | --- | --- | --- |
| SignalForge CLI recorder | `p7-scanner` | tag `signalforge-cli-v*` | Windows/macOS/Linux `signalforge` binaries attached to the `p7-scanner` GitHub Release | `signalforge.org/#recorder`, CLI updater |
| Hub container images | `p7-scanner` | push to `main` / workflow dispatch | GHCR images and `signalforge.org/p7-scanner-update.json` | deployed hubs, admin update checks |
| Legacy recorder UI | `p7-scanner` | manual workflow dispatch only | build artifacts for internal/manual testing | none by default |

## What We Are Not Doing

- No new official recorder app releases from `signalforge.org`.
- No official binary releases from `signalforge-node`.
- No new automatic `ui-v*` desktop recorder publishing.

## Release Names

- CLI recorder tags: `signalforge-cli-v0.1.0`
- Hub image updates: commit-derived image tags in `p7-scanner-update.json`
- Legacy recorder UI builds: manual only, not a public release channel

The public site and CLI updater must filter `p7-scanner` releases by the `signalforge-cli-v*` tag prefix. Do not use the repository-wide `latest` release, because older hub/recorder tags such as `v1.2.x` may also exist in `p7-scanner`.
