# Fair Source Alignment

SignalForge Node is licensed under the GNU Affero General Public License v3.0 or later. It is open source software, not a proprietary Fair Source License project.

The project is fair-source aligned in the practical sense described by Fair.io: the source is public, community participation is welcome, and governance is explicit about protecting the producer, operators, and users from closed resale or confusing unofficial deployments.

## What This Means

- Anyone can read, run, fork, modify, and redistribute the code under AGPLv3.
- Modified network services must offer their users the corresponding source code.
- Commercial hosting, setup, hardware, or support is not automatically forbidden by AGPLv3.
- Closed proprietary forks and hidden hosted modifications are not compatible with the license.
- Official, verified, listed, directory, update, package, and trademark status remain controlled by maintainers.
- Operators must not imply their node is official unless maintainers explicitly grant that status.

## Community Intent

SignalForge is meant to be shared infrastructure for people running and connecting community scanner nodes. The project should not become a private toll booth where operators sell each other closed access to the same public code without giving users source, provenance, and honest status.

The source stays available. The network stays trust-aware. The official SignalForge name stays protected.

## For Operators

If you run a public or shared node, keep a visible source/license link available to users of that service. If you modify the node, publish the corresponding source for the version you operate.

The web console footer includes source, license, and Fair Source links. Forks and modified builds should set these Vite build variables so users are sent to the corresponding source for that deployed service:

```text
VITE_SIGNALFORGE_SOURCE_REPO_URL=https://github.com/your-org/your-node
VITE_SIGNALFORGE_SOURCE_URL=https://github.com/your-org/your-node/tree/<deployed-ref>
VITE_SIGNALFORGE_LICENSE_URL=https://github.com/your-org/your-node/blob/<deployed-ref>/LICENSE
VITE_SIGNALFORGE_FAIR_SOURCE_URL=https://github.com/your-org/your-node/blob/<deployed-ref>/FAIR-SOURCE.md
```

Operators who want a separate commercial relationship, official status, or promoted listing should contact the maintainers before presenting themselves as part of the official SignalForge network.