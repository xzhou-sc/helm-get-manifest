// Command get-manifest is a Helm plugin that filters the rendered manifest
// stored for an installed release by the template that produced it.
//
// It does not re-render the chart: everything comes from what Helm has stored
// for the release, so it works when the chart is unavailable, has moved on, or
// when inspecting an older revision.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/xzhou-sc/helm-get-manifest/internal/manifest"
)

// Exit codes. Distinct values let scripts tell "no such source" apart from a
// release lookup failure.
const (
	exitOK           = 0
	exitUsage        = 2
	exitHelmFailed   = 3
	exitSourceNotFnd = 4
	exitAmbiguous    = 5
)

const usage = `helm get-manifest filters the rendered manifest stored for an installed
Helm release, selecting documents by the template that produced them.

Usage:
  helm get-manifest RELEASE [SOURCE] [flags]

SOURCE selects the documents rendered from one template. It may be the full
path Helm records, or any trailing part of it that is unambiguous, so
"deployment" or "deployment.yaml" resolves
"my-chart/templates/deployment.yaml". The .yaml/.yml extension is optional.

Flags:
  -s, --source SOURCE         same as passing SOURCE positionally
  -c, --clean                 strip Helm source comments and the leading
                              document separator, keeping separators between
                              multiple documents
  -l, --list                  list the distinct sources, one per line
  -r, --revision int          inspect a specific stored revision
  -n, --namespace string      namespace scope for this request
  -k, --kube-context string   name of the kubeconfig context to use
  -h, --help                  show this help

Examples:
  helm get-manifest my-release
  helm get-manifest my-release deployment
  helm get-manifest my-release my-chart/templates/deployment.yaml
  helm get-manifest my-release deployment -c | yq '.spec'
  helm get-manifest my-release deployment -r 12
  helm get-manifest my-release -l | fzf
`

type options struct {
	release     string
	source      string
	clean       bool
	listSources bool
	revision    string
	namespace   string
	kubeContext string
}

func main() {
	// Behave like a normal Unix filter when the reader goes away, e.g. under
	// `| head`: die quietly instead of printing a write error.
	signal.Reset(syscall.SIGPIPE)

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			fmt.Fprint(os.Stdout, usage)
			os.Exit(exitOK)
		}
		fmt.Fprintf(os.Stderr, "helm get-manifest: %v\n\n%s", err, usage)
		os.Exit(exitUsage)
	}

	code, err := run(opts, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm get-manifest: %v\n", err)
	}
	os.Exit(code)
}

func run(opts *options, out *os.File) (int, error) {
	raw, err := fetchManifest(opts)
	if err != nil {
		return exitHelmFailed, err
	}

	docs := manifest.Split(raw)

	if opts.listSources {
		for _, s := range manifest.Sources(docs) {
			fmt.Fprintln(out, s)
		}
		return exitOK, nil
	}

	if opts.source != "" {
		source, err := manifest.Resolve(docs, opts.source)
		var ambiguous *manifest.ErrAmbiguous
		switch {
		case errors.As(err, &ambiguous):
			return exitAmbiguous, fmt.Errorf("%w\n\nmatches:\n  %s",
				err, strings.Join(ambiguous.Matches, "\n  "))
		case err != nil:
			return exitSourceNotFnd, err
		case source == "":
			return exitSourceNotFnd, sourceNotFound(docs, opts.source)
		}
		docs = manifest.Select(docs, source)
	}

	fmt.Fprint(out, manifest.Render(docs, opts.clean))
	return exitOK, nil
}

// sourceNotFound builds an error that suggests near matches, so a user who
// typed a bare filename or the wrong subchart path can see the real path.
func sourceNotFound(docs []manifest.Doc, want string) error {
	msg := fmt.Sprintf("source not found: %s", want)
	if c := manifest.Candidates(docs, want); len(c) > 0 {
		msg += "\n\navailable matches:\n  " + strings.Join(c, "\n  ")
	}
	return errors.New(msg)
}

// fetchManifest shells out to Helm for the stored manifest.
//
// Helm exports HELM_BIN to plugins, so this re-invokes the exact binary that
// loaded us rather than guessing at PATH. Only "get manifest" is ever called,
// which is a Helm builtin, so there is no risk of recursing into this plugin.
func fetchManifest(opts *options) (string, error) {
	bin := os.Getenv("HELM_BIN")
	if bin == "" {
		bin = "helm"
	}

	args := []string{"get", "manifest", opts.release}
	if opts.revision != "" {
		args = append(args, "--revision", opts.revision)
	}
	// Namespace and context are passed through explicitly when given. When
	// they are not, Helm's own HELM_NAMESPACE / HELM_KUBECONTEXT environment
	// carries the invoking context through to the subprocess.
	if opts.namespace != "" {
		args = append(args, "--namespace", opts.namespace)
	}
	if opts.kubeContext != "" {
		args = append(args, "--kube-context", opts.kubeContext)
	}

	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr // let Helm report release lookup failures itself
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("helm get manifest failed: %w", err)
	}
	return string(out), nil
}

var errHelp = errors.New("help requested")

// parseArgs reads the command line by hand rather than through the flag
// package, which cannot express "-n value" alongside "--namespace=value" and
// would reject Helm's flags appearing after the release name.
func parseArgs(args []string) (*options, error) {
	opts := &options{
		// Helm exports the invoking namespace and context to plugins. Seed
		// from them so the plugin defaults to the same release the equivalent
		// `helm get manifest` would find.
		namespace:   os.Getenv("HELM_NAMESPACE"),
		kubeContext: os.Getenv("HELM_KUBECONTEXT"),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Split --flag=value so both spellings work.
		name, inline, hasInline := strings.Cut(arg, "=")
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag needs an argument: %s", name)
			}
			i++
			return args[i], nil
		}

		var err error
		switch name {
		case "-h", "--help":
			return nil, errHelp
		case "-c", "--clean":
			opts.clean = true
		case "-l", "--list":
			opts.listSources = true
		case "-s", "--source":
			if opts.source != "" {
				return nil, errors.New("source given twice")
			}
			opts.source, err = value()
		case "-r", "--revision":
			opts.revision, err = value()
		case "-n", "--namespace":
			opts.namespace, err = value()
		case "-k", "--kube-context":
			opts.kubeContext, err = value()
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown flag: %s", name)
			}
			// RELEASE first, then an optional SOURCE.
			switch {
			case opts.release == "":
				opts.release = arg
			case opts.source == "":
				opts.source = arg
			default:
				return nil, fmt.Errorf("unexpected argument: %s (source given twice?)", arg)
			}
		}
		if err != nil {
			return nil, err
		}
	}

	if opts.release == "" {
		return nil, errors.New("a release name is required")
	}
	if opts.listSources && opts.source != "" {
		return nil, errors.New("--list and a source are mutually exclusive")
	}
	return opts, nil
}
