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
