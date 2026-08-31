# helm-get-manifest

`helm-get-manifest` filters the rendered manifest stored for an installed Helm release.

It adds source-aware querying similar to `helm template --show-only`, but operates on
`helm get manifest` output rather than re-rendering the chart.

```bash
helm get-manifest my-release \
  --source my-chart/templates/deployment.yaml \
  --clean |
yq '.spec'
```

## Why

`helm get manifest` returns everything a release rendered, with no way to narrow it to
one template. `helm template --show-only` can do that, but only by re-rendering the
chart locally.

This plugin never re-renders. It reads what Helm stored for the release, which is what
you want when:

- the local chart is unavailable, or its version has moved on;
- the local values no longer match what was installed;
- the release was installed by another system;
- you are inspecting an older revision;
- reproducing the original render environment is inconvenient or impossible.

It replaces the usual ad-hoc `awk`/`yq` filter over `# Source:` comments.

## Install

```bash
helm plugin install https://github.com/xzhou-sc/helm-get-manifest
```

Go is required: the plugin builds its binary at install time.

## Usage

```
helm get-manifest RELEASE [flags]
```

| Flag | Description |
| --- | --- |
| `--source SOURCE` | Only emit documents rendered from `SOURCE`, matched exactly against the `# Source:` annotation. |
| `--clean` | Strip Helm source comments and the leading document separator, for piping. |
| `--list-sources` | List the distinct sources, one per line. |
| `--revision N` | Inspect a specific stored revision. |
| `-n`, `--namespace` | Namespace scope for this request. |
| `--kube-context` | Name of the kubeconfig context to use. |

Namespace and context default to the ones Helm was invoked with, so the plugin resolves
the same release `helm get manifest` would.

### Examples

The whole stored manifest, byte for byte what `helm get manifest` returns:

```bash
helm get-manifest my-release
```

Just the documents one template produced:

```bash
helm get-manifest my-release --source my-chart/templates/deployment.yaml
```

Pipe-friendly output:

```bash
helm get-manifest my-release --source my-chart/templates/deployment.yaml --clean |
  yq '.spec.template.spec.containers'

helm get-manifest my-release --source my-chart/templates/deployment.yaml --clean |
  kubectl diff -f -
```

Find the source you want:

```bash
helm get-manifest my-release --list-sources | grep secret
helm get-manifest my-release --list-sources | fzf
```

Inspect a past revision:

```bash
helm get-manifest my-release --revision 12 --source my-chart/templates/deployment.yaml
```

### Source paths

Sources are matched exactly, as Helm writes them. For subcharts that includes the full
chart path, so a parent and a subchart template of the same name stay distinct:

```
my-chart/templates/secret.yaml
my-chart/charts/sub/templates/secret.yaml
```

Use `--list-sources` to see the exact strings. If an exact match fails, near matches are
suggested on stderr:

```
$ helm get-manifest my-release --source secret.yaml
helm get-manifest: source not found: secret.yaml

available matches:
  my-chart/templates/secret.yaml
  my-chart/charts/sub/templates/secret.yaml
```

## Behaviour

**Output is preserved.** Without `--clean`, selected documents are emitted exactly as
Helm stored them: no reordering, re-indenting, re-quoting or comment loss. The plugin is
a filter, not a formatter. With no `--source`, output is byte-identical to
`helm get manifest`.

**`--clean` does the minimum.** It removes Helm's `# Source:` line and the leading `---`,
and nothing else. Unrelated comments survive. Separators *between* documents are kept, so
a template that renders several resources still yields a valid multi-document stream:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: foo-a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: foo-b
```

**Document boundaries are real.** A `---` inside a block or quoted scalar is content, not
a separator, and a `# Source:` inside a resource body is data, not provenance:

```yaml
data:
  script: |
    echo hello
    ---
    echo world
```

stays one document.

**Unix conventions.** Selected manifests go to stdout, diagnostics to stderr, and a closed
pipe (`| head`) exits quietly.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `2` | Invalid usage. |
| `3` | Helm failed to return the release manifest. |
| `4` | The requested source was not found. |

## Development

```bash
go test ./...
go build -o bin/get-manifest ./cmd/get-manifest
helm plugin install .
```

Parser and CLI tests run from fixtures and need no cluster.

## License

Apache-2.0
