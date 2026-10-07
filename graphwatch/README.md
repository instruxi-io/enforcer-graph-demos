# graphwatch

A terminal viewer for a plan held in Enforcer Graph. Standard library only.

```bash
go run . --demo --layout mycelium      # offline: no API, no key
go run . --graph <graph-id>            # watch a real graph
go run . --demo                        # create a demo graph in your workspace and work it
```

## What it draws

- **layers** (default): prerequisites sit above what they unblock, ordered by a
  Sugiyama barycenter pass. Nodes are dots shaded by `work_state`. Work shows as
  heat: a pulse travels down an edge into a node when its run starts and out
  when it finishes, and everything cools when the plan goes quiet.
- **mycelium** (`--layout mycelium`): a radial view with the roots on an inner
  ring and depth as radius. Hyphae grow from prerequisite to dependent with a
  neighbour-sensing model (steered tips with persistence, attraction to
  targets, negative autotropism via an occupancy field, lateral branches 30 to
  70 degrees off), and fuse on contact. Growth is seeded from node ids and
  cached; a frame only chooses how much to reveal and in what colour.

Rendering is a Braille sub-pixel canvas (2 by 4 dots per cell) under a text
layer. Colours are 24-bit, quantised to 16 levels per channel, and only changed
cells are written each frame.

## Flags

| flag | default | meaning |
|---|---|---|
| `--graph` | | graph id to watch |
| `--demo` | false | create a graph, grow it and work it while watching; with `--layout mycelium` an offline demo that needs no API |
| `--size` | 36 | demo: nodes to plan |
| `--workers` | 4 | demo: concurrent harnesses |
| `--stay` | false | keep watching after the plan completes |
| `--fps` | 15 | frames per second (only changed cells are sent) |
| `--motion` | events | `events`: motion follows graph events and a quiet graph stops drawing; `continuous`: running nodes pulse |
| `--layout` | layers | `layers` or `mycelium` |
| `--no-overlays` | false | plain header: drop the epoch, work_state counts, review holds, recruiting demand and stream cursor |
| `--base` | `$GRAPH_BASE_URL` or `https://api.instruxi.dev` | API origin |

## Auth

`GRAPH_AUTH_HELPER` names a command that prints a JSON object of request
headers; it is run again on a 401, so a helper that prints a fresh OAuth bearer
keeps the viewer signed in. The `enforcer` Claude Code plugin ships one
(`bin/enforcer-headers.mjs`). Otherwise set `GRAPH_API_KEY`.

The live `--demo` creates its graph through `POST /graphs/import`, so a sign-in
holding `enforcer:graph-graph-import.write` is enough; it does not need
`graph-graphs.write`.

## Tests

`go test ./...` runs unit tests and golden tests that pin the layered layout and
the mycelium growth byte for byte. Growth uses no clock and no randomness, so a
changed golden means a changed model: regenerate it on purpose.
