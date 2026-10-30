# managerkit

`github.com/confighub/sdk/managerkit` holds the shared parts of a manager tool: a command-line tool that manages one concern across a fleet of ConfigHub Units, such as namespaces, workload scheduling, autoscaling or observability. Each such tool knows its own concern -- which resources to read, which functions to run and what rules to enforce -- and gets the rest from this module: where shared policy lives, how a policy pack is installed and reported, how reusable edits are stored and applied, and how a write reports what it changed. Tools built this way have the same commands, flags and output, so an operator, or an agent, that has used one can use the next.

The [manager tools in the examples repository](https://github.com/confighub/examples), such as `scheduling-manager`, `autoscale-manager` and `namespace-manager`, are built on it.

## What it is not

Reading a fleet is [`core/cubapi`](https://pkg.go.dev/github.com/confighub/sdk/core/cubapi): `SnapshotLoader` joins a Unit query with a resource query, and `MemoizedClient` caches the reads. The generic command-line surface is [`cliutil`](https://pkg.go.dev/github.com/confighub/sdk/cliutil): `QueryFlags` and its Space-label shorthands, output parsing and tables. Rendering a ConfigDiff is [`cubutil/diffview`](https://pkg.go.dev/github.com/confighub/sdk/cubutil/diffview). What is in managerkit is a convention of manager tools rather than a ConfigHub primitive, which is why it is a module of its own.

## Packages

- **`managerkit`** -- `CommonSpace`, the Space (`common`) where manager tools keep what an organization shares: guardrail Triggers, the profile library, and the Attributes a validating function reads. One Space rather than one per tool, because a Space selects guardrails with one WhereTrigger naming one policy Space; every command that writes there takes a flag to put it elsewhere. Also `AddOutputFlag` and `Render`, the `-o/--output` flag every command has. It defaults to JSON rather than a table, because these tools are read by agents as often as by people; `json`, `yaml`, `table`, `jq=<expr>` and `yq=<expr>` are accepted.
- **`managerkit/guardrails`** -- a tool's policy pack: a group of validating Triggers defined once in the policy Space and selected by the WhereTrigger of each Space, or Target, that opts into the group. A tool supplies a `Pack` of `Rule`s, each a CEL expression over the resource `r` run by `vet-cel`, or another validating function; the package supplies `guardrails install` (dry run by default, `--commit` to apply) and `guardrails status`, and for a rule that needs more than one resource to decide, `guardrails annotate`, which writes the tool's findings onto each Unit as an annotation the rule then checks. A resource is excepted from one rule by the annotation `exceptions.guardrails.confighub.com/<rule>`, whose value says why. Rules install as warnings and are made blocking with `cub trigger update <slug> --space <policy-space> --unwarn`. A Space that already selects Triggers its own way is reported and left alone.
- **`managerkit/profiles`** -- a library of profiles: named, reusable edits stored as ConfigHub Invocations, each a function with preset arguments and optional parameters supplied at apply time. A tool declares a `Library` of `Spec`s and gets `profile install`, `profile list` and `profile apply <profile> <space>/<unit>`, plus `fleet-edit --profile <slug>` to apply one profile to every Unit a filter selects.
- **`managerkit/write`** -- the reporting half of a mutating command: `ParseUnitRef` for the `<space>/<unit>` argument, `Change` for the change description and dry run, and `ReportMutations`, which renders the function results per Unit, followed on a table by the diff of each Unit that changed or, on a dry run, would change.

## Example

A tool declares its pack and library and adds the commands they build to its own:

```go
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/managerkit/guardrails"
	"github.com/confighub/sdk/managerkit/profiles"
)

func preflight(ctx context.Context) (*cubapi.Client, error) {
	// Verify the ConfigHub session and return a client.
	return nil, nil
}

var pack = guardrails.Pack{
	Group: "scheduling",
	Rules: []guardrails.Rule{{
		Slug:        "deployment-needs-node-selector",
		Description: "Warns on a Deployment with no nodeSelector.",
		Expression:  "r.kind != 'Deployment' || has(r.spec.template.spec.nodeSelector)",
	}},
}

var library = profiles.Library{
	Tool:           "scheduling-manager",
	Noun:           "placement profile",
	Target:         "a workload",
	Preflight:      preflight,
	InvocationName: func() string { return "cub-scheduling" },
	Profiles: []profiles.Spec{{
		Slug:        "node-pool",
		Description: "set-yq: pin to the node pool named by --param pool",
		Function:    "set-yq",
		Args: []api.FunctionArgument{
			profiles.Arg("yq-expression", `.spec.template.spec.nodeSelector.pool = $params.pool`),
			profiles.YQParamArg("pool"),
		},
		Params: []profiles.Param{{Name: "pool"}},
	}},
}

func addCommands(root *cobra.Command) {
	guardrailsCmd := &cobra.Command{Use: "guardrails", Short: "Install and inspect the scheduling policy pack"}
	guardrailsCmd.AddCommand(pack.InstallCmd(preflight), pack.StatusCmd(preflight))
	root.AddCommand(guardrailsCmd, library.Command())
}
```

The rule opens with a kind test so that it passes for every resource it does not govern; a rule that fails everything else it is run on blocks those Units once it is made blocking.
