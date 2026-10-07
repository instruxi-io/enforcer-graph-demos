# enforcer-graph demos

Two small viewers for a plan held in [Enforcer Graph](https://github.com/instruxi-io/claude-plugins):
a tenant-scoped DAG service where agents claim nodes, do the work, and have each
completion judged against acceptance criteria before anything downstream unblocks.

| directory | what it is | needs an account? |
|---|---|---|
| [`graphwatch/`](./graphwatch) | A terminal viewer. Draws a graph as layered dots (or as a growing mycelium), animated from the graph's event stream. Go, standard library only. | No for the offline mycelium demo; yes to watch or demo a real graph |
| [`graph-live/`](./graph-live) | A browser viewer. The same picture in React, plus a demo mode that creates a graph and works it the way a set of harnesses would. The minimal reference for the published `@instruxi-io/graph-hooks`. | Yes |

## Try it in thirty seconds, no account

```bash
cd graphwatch
go run . --demo --layout mycelium
```

A three-root, depth-four plan is worked in-process and drawn as hyphae growing
from the roots to the leaves. Nothing leaves your machine.

## Install graphwatch without Go

Every version tag publishes graphwatch as a single static binary for Linux,
macOS and Windows on amd64 and arm64, with a `SHA256SUMS` file, on the
[releases page](https://github.com/instruxi-io/enforcer-graph-demos/releases).

```bash
# macOS on Apple silicon; swap the suffix for linux-amd64, linux-arm64, darwin-amd64
curl -fsSLO https://github.com/instruxi-io/enforcer-graph-demos/releases/latest/download/graphwatch-darwin-arm64
curl -fsSLO https://github.com/instruxi-io/enforcer-graph-demos/releases/latest/download/SHA256SUMS
grep graphwatch-darwin-arm64 SHA256SUMS | shasum -a 256 -c
chmod +x graphwatch-darwin-arm64 && mv graphwatch-darwin-arm64 /usr/local/bin/graphwatch
graphwatch --demo --layout mycelium
```

On macOS a downloaded binary is quarantined; clear it with
`xattr -d com.apple.quarantine /usr/local/bin/graphwatch` if Gatekeeper refuses it.
To vendor it into another repository, commit the binary for your platform with
its checksum line, or pin a release tag and download it in CI.

## Watch a real graph

Both viewers talk to the graph API at `https://api.instruxi.dev` (override with
`GRAPH_BASE_URL`) and authenticate with an API key in `GRAPH_API_KEY`. Get a key
from your Enforcer workspace, or sign in with the `enforcer` Claude Code plugin
and let `graphwatch` borrow that session (`GRAPH_AUTH_HELPER`, see its README).

```bash
# terminal
cd graphwatch && go run . --graph <graph-id>
go run . --demo              # creates a demo graph in your workspace and works it

# browser
cd graph-live
GITHUB_TOKEN=$(gh auth token) npm install   # @instruxi-io packages live on GitHub Packages
GRAPH_API_KEY=... npm run dev                # http://127.0.0.1:5178
```

The browser demo's writes go out under your key, so a demo graph is an ordinary
graph in your workspace named `demo-<scenario>-<time>`. Nothing is archived
automatically.

## What you are looking at

- A **dot** is a node. Its colour is the server's `work_state`: looking for work,
  claimed, looking for validation or arbitration, waiting, held for review, done.
- An **edge** points from a dependent to its prerequisite in the API; both viewers
  flip it so work flows downward and outward on screen.
- **Heat** is activity: a pulse travels into a node when a run starts and out when
  it finishes. A quiet plan stops drawing.
- A node is **done** only after its run was judged against the acceptance
  criteria frozen at claim time. A rejected run sends the node back to the
  frontier with its dependents still blocked.

## Layout

```
graphwatch/   Go module, `go test ./...`
graph-live/   Vite + React app, `npm run build` needs no key
AGENTS.md     orientation for an agent working in this repository
```

See [AGENTS.md](./AGENTS.md) before changing anything.

## Licence and scope

The demo clients in this repository are MIT licensed (see [LICENSE](./LICENSE)). The Enforcer Graph service and its API are not part of this licence and are not open source; using these viewers against a real plan requires an Enforcer account.

`graph-live` builds on `@instruxi-io/graph-hooks`, which is distributed to Instruxi organisation members only. Outside the organisation, `graphwatch` (standard library only) is the client you can build and run, including the offline demo.
