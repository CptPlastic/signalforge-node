# Source Mirror Policy

`signalforge-node` is the clean public source mirror for the buildable node stack.

## Include

- Go server and database migrations.
- React web console.
- Recorder clients and build scripts.
- Dockerfiles and compose files for local, Plesk, Portainer, and peer-stack deployments.
- Operator documentation and SignalHub federation docs.
- Example environment files with safe placeholders.

## Exclude

- Real `.env` files.
- API keys, source upload keys, mail credentials, webhook URLs, database passwords, and private deployment notes.
- Generated build output, local volumes, call audio, or machine-specific config.
- Any metadata that makes one operator's node look official by default.

## Clients Are Part Of The Node

The public mirror intentionally includes clients:

- `client/` is the web console operators use to run a hub.
- `tools/p7-recorder-go/` and recorder UI tools turn local audio into node sources.
- Docker and compose files tie the server and client together in a deployable shape.

The goal is a repo a non-developer operator can inspect, build, and run without needing private Project Seven deployment context.
