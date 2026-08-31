# helm-get-manifest

A Helm plugin that filters the rendered manifest stored for an installed release,
selecting just the documents one template produced.

Like `helm template --show-only`, but against the release Helm already has —
nothing is re-rendered.

```console
$ helm get-manifest my-release deployment.yaml
---
# Source: my-chart/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
...
```

## Why

`helm get manifest` gives you everything a release rendered, with no way to narrow it
to one template. `helm template --show-only` can narrow, but only by re-rendering the
chart locally — which needs the chart, the values, and the environment that produced
the release in the first place.

This reads what Helm stored, so it still works when the chart has moved on, the values
have changed, someone else did the install, or you are looking at an older revision.

It replaces the `awk`-over-`# Source:`-comments one-liner everyone keeps rewriting.

## Install

```bash
helm plugin install https://github.com/xzhou-sc/helm-get-manifest
```

Prebuilt binaries are included for Linux, macOS and Windows on amd64 and arm64.

On Helm 4, installing straight from the repository rather than a release needs
`--verify=false`, since a git source carries no signature.

## Usage

```
helm get-manifest RELEASE [SOURCE] [flags]
```

| Flag | Description |
| --- | --- |
| `-s`, `--source SOURCE` | Same as passing `SOURCE` positionally. |
| `-c`, `--clean` | Strip source comments and the leading `---`, for piping. |
| `-l`, `--list` | List the sources in the manifest, one per line. |
| `-r`, `--revision N` | Inspect a stored revision. |
| `-n`, `--namespace` | Namespace scope for this request. |
| `-k`, `--kube-context` | Kubeconfig context to use. |

Namespace and context default to the ones Helm was invoked with, so this resolves the
same release `helm get manifest` would.

## Examples

```bash
# the whole stored manifest, byte for byte what `helm get manifest` returns
helm get-manifest my-release

# one template's output, ready to pipe
helm get-manifest my-release deployment.yaml -c | yq '.spec.template.spec.containers'
helm get-manifest my-release deployment.yaml -c | kubectl diff -f -

# find the source you want
helm get-manifest my-release -l | grep secret
helm get-manifest my-release -l | fzf

# a past revision
helm get-manifest my-release deployment.yaml -r 12
```

## Selecting a source

Give the full path Helm records, or any unambiguous trailing part of it. The
`.yaml`/`.yml` extension is optional, since charts are inconsistent about which they
use — these all select the same template:

```bash
helm get-manifest my-release deployment
helm get-manifest my-release templates/deployment.yaml
helm get-manifest my-release my-chart/templates/deployment.yaml
```

Charts reuse template names across subcharts, so a short name can be ambiguous. It is
reported rather than guessed at:

```console
$ helm get-manifest my-release secret.yaml
helm get-manifest: source is ambiguous: secret.yaml

matches:
  my-chart/templates/secret.yaml
  my-chart/charts/sub/templates/secret.yaml
```

Add enough leading path to make it unique — `sub/templates/secret.yaml` resolves. Run
`-l` to see the exact strings, and if nothing matches, near misses are suggested on
stderr.

## Behaviour

**Output is preserved.** Without `-c`, documents come out exactly as Helm stored them:
no reordering, re-indenting, re-quoting or comment loss. This is a filter, not a
formatter. With no source given, output is byte-identical to `helm get manifest`.

**`-c` does the minimum.** It drops Helm's `# Source:` line and the leading `---`, and
nothing else — unrelated comments survive. Separators *between* documents are kept, so
a template rendering several resources stays a valid multi-document stream.

**Document boundaries are real.** A `---` inside a block or quoted scalar is content,
and a `# Source:` inside a resource body is data. This stays one document:

```yaml
data:
  script: |
    echo hello
    ---
    echo world
```

**Unix conventions.** Manifests to stdout, diagnostics to stderr, and a closed pipe
(`| head`) exits quietly.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `2` | Invalid usage. |
| `3` | Helm failed to return the release manifest. |
| `4` | Source not found. |
| `5` | Source was ambiguous. |

## Development

```bash
go test ./...
helm plugin install .
```

Tests run from fixtures and need no cluster.

## License

Apache-2.0
