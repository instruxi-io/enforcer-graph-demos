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

## Commands

`graphwatch` with no arguments opens the picker; `graphwatch <command>` runs one
of the commands below. All of them are read-only (GET only). Global flags:
`--base <url>  --json  --limit <n>  --no-color  --api-key-file <path>`. Put
flags before the positional arguments (`graphwatch nodes --json <graph>`).
`graphwatch help <command>` lists a command's flags.

| command | what it does | worked example |
|---|---|---|
| `graphwatch whoami` | the account, tenant and role the API resolves, and where the credential came from | `graphwatch whoami` |
| `graphwatch graphs` | list the graphs you can see, or `graphs show <id\|slug>` for one with its roll-up | `graphwatch graphs` |
| `graphwatch nodes` | list a graph's tasks (`--status`, `--type`), or `nodes show <graph> <key\|id>` with its runs | `graphwatch nodes --status active <graph>` |
| `graphwatch edges` | list a graph's dependencies; `--as-flow` prints prerequisite to dependent | `graphwatch edges --as-flow <graph>` |
| `graphwatch runs` | a node's runs and how they were judged; `runs show <graph> <node> <run> [--full]` | `graphwatch runs <graph> <node>` |
| `graphwatch review` | human review items holding a graph (`--open`, `--all`); `review show <graph> <item>` | `graphwatch review <graph>` |
| `graphwatch epochs` | replay history; `epochs show <graph> <epoch>` | `graphwatch epochs <graph>` |
| `graphwatch access` | who can reach a graph (`--full-ids`) | `graphwatch access <graph>` |
| `graphwatch recruiting` | what one graph recruits for | `graphwatch recruiting <graph>` |
| `graphwatch work` | the work lobby: graphs looking for workers or validation (`--state`, `--kind`, `--graph`, `--watch secs`) | `graphwatch work --kind work` |
| `graphwatch query` | query nodes or runs across every graph you can see; `--all` follows `next` | `graphwatch query nodes --all` |
| `graphwatch tail` | follow a graph's event stream as text; `--save-cursor` resumes next run | `graphwatch tail --save-cursor <graph>` |
| `graphwatch help` | list commands, or describe one | `graphwatch help runs` |

Output below is copied from a real run against a local fake API (ids shortened,
no credentials). Real output depends on your graph.

```text
$ graphwatch graphs
ID        SLUG   NAME        MODE  LIFECYCLE
11111111  alpha  Alpha plan  dag   open

$ graphwatch nodes <graph>
KEY     TYPE  STATUS  WORK_STATE        TIER  TITLE
design  task  done    finished          -     Design
build   task  active  looking_for_work  -     Build

$ graphwatch nodes --status active <graph>
KEY    TYPE  STATUS  WORK_STATE        TIER  TITLE
build  task  active  looking_for_work  -     Build

$ graphwatch edges <graph>
FROM_KEY  RELATION    TO_KEY  TYPE
build     depends on  design  requires

$ graphwatch edges --as-flow <graph>
PREREQUISITE  ->  DEPENDENT  TYPE
design        ->  build      requires
```

The `requires` edge points from the dependent to its prerequisite; `edges`
labels it "depends on", and `--as-flow` flips it for reading.

`runs`, `review`, `epochs`, `access`, `recruiting`, `work`, `query` and `tail`
follow the same pattern: `graphwatch help <command>` gives the exact flags, and
each prints a table and accepts `--json`.

### `--json` piping

`--json` prints the API's data payload, so it pipes into `jq`:

```bash
graphwatch nodes --json <graph> | jq -r '.[].key'
graphwatch nodes --json <graph> | jq '[.[] | select(.work_state == "looking_for_work")] | length'
graphwatch graphs --json | jq -r '.[].slug'
```

```text
$ graphwatch nodes --json <graph> | jq -r '.[].key'
design
build
```

## Interactive picker

`graphwatch` with no arguments lists your graphs.

| key | action |
|---|---|
| `up` / `k`, `down` / `j` | move the cursor |
| `/` | filter the list (Esc clears the filter) |
| `enter` | watch the graph (layers) |
| `m` | watch the graph as mycelium |
| `t` | tail the graph's events |
| `r` | refresh the list |
| `q` / `esc` | quit |

## Watch view: inspector and header

In the watch view the arrow keys (or `h j k l`) move a selection to the nearest
node, `enter` opens the inspector (the node's latest run, evidence, verdicts,
tier and route) and `esc` closes it. While it is open, moving the selection
shows the next node, and an event for that node refetches it.

The header shows the epoch, work_state counts, review holds, recruiting demand
and the stream cursor. `--no-overlays` drops it back to a plain header.

## Auth

Every command and the watch view use one resolution order, from `auth.go`. The
first source that is set wins:

1. `--api-key-file <path>`: the key is read from that file
2. `GRAPH_AUTH_HELPER`: a command that prints a JSON object of request headers; run again on a 401
3. `GRAPH_API_KEY`
4. `ENFORCER_API_KEY`
5. the `enforcer` Claude Code plugin's sign-in, through its header helper
6. `enforcer.api_key` in `~/.enforcer/credentials.json`

The API origin is `--base`, then `GRAPH_BASE_URL`, then the plugin's saved
`base_url`, then `https://api.instruxi.dev`.

graphwatch will never refresh OAuth itself: the plugin's helper rotates a
single-use refresh token and writes it back, so refreshing here, or writing
`credentials.json`, would sign the plugin out. Sign in with `/enforcer:login`
in Claude Code. No command prints a key, token or Authorization header.

`graphwatch whoami` shows which source won:

```text
$ graphwatch whoami
FIELD       VALUE
base url    http://127.0.0.1:8765/api/v1/graph
credential  GRAPH_API_KEY
account     dev@example.com
tenant      Example Workspace
role        admin
```

The live `--demo` creates its graph through `POST /graphs/import`, so a sign-in
holding `enforcer:graph-graph-import.write` is enough; it does not need
`graph-graphs.write`.

## Tests

`go test ./...` runs unit tests and golden tests that pin the layered layout and
the mycelium growth byte for byte. Growth uses no clock and no randomness, so a
changed golden means a changed model: regenerate it on purpose.
