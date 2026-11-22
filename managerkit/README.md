# managerkit

`github.com/confighub/sdk/managerkit` holds the shared parts of a manager tool: a command-line tool that manages one concern across a fleet of ConfigHub Units, such as namespaces, workload scheduling, autoscaling or observability. Each such tool knows its own concern -- which resources to read, which functions to run and what rules to enforce -- and gets the rest from this module: where shared policy lives, how a policy pack is installed and reported, how reusable edits are stored and applied, and how a write reports what it changed. Tools built this way have the same commands, flags and output, so an operator, or an agent, that has used one can use the next.

[cub-manage](https://github.com/confighub/cub-manage), a `cub` plugin that manages Kubernetes namespaces, RBAC, network policy, workloads, scheduling, autoscaling and observability as one command group each, is built on it.

## What it is not

Reading a fleet is [`core/cubapi`](https://pkg.go.dev/github.com/confighub/sdk/core/cubapi): `SnapshotLoader` joins a Unit query with a resource query, and `MemoizedClient` caches the reads. The generic command-line surface is [`cliutil`](https://pkg.go.dev/github.com/confighub/sdk/cliutil): `QueryFlags` and its Space-label shorthands, output parsing and tables. Rendering a ConfigDiff is [`cubutil/diffview`](https://pkg.go.dev/github.com/confighub/sdk/cubutil/diffview). What is in managerkit is a convention of manager tools rather than a ConfigHub primitive, which is why it is a module of its own.

## Packages

- **`managerkit`** -- `CommonSpace`, the Space (`common`) where manager tools keep what an organization shares: guardrail Triggers, the profile library, and the Attributes a validating function reads. One Space rather than one per tool, because a Space selects guardrails with one WhereTrigger naming one policy Space; every command that writes there takes a flag to put it elsewhere. Also `AddOutputFlag` and `Render`, the `-o/--output` flag every command has. And `BindComponentResolver`, which lets `--component` name a Component by slug in a command whose flags come from `cliutil.QueryFlags`. It defaults to JSON rather than a table, because these tools are read by agents as often as by people; `json`, `yaml`, `table`, `jq=<expr>` and `yq=<expr>` are accepted.
- **`managerkit/guardrails`** -- a tool's policy pack: a group of validating Triggers defined once in the policy Space and selected by the WhereTrigger of each Space, or Target, that opts into the group. A tool supplies a `Pack` of `Check`s, each with a control number of its own (`CH-NNNN`), a name, a default priority, the resource types it governs, and how it runs: a CEL list of failures over the resource `r`, run by `vet-cel`; a Pod Security Standards check; a ValidatingAdmissionPolicy from a library the pack adopts (`vap`), which install writes into the policy Space as a library Unit, replaced on every install, and a params Unit, created once with the library's defaults so a team's edits survive, both passed to `vet-validating-admission-policy`; or another validating function, which, if it runs several checks chosen by a `check` argument as `vet-correctness` does, takes the name the function knows the check by (`FunctionCheck`), so that the checks of one function share a rule. A finding carries its check's name as its Issue Identifier, or for a function's check, the `FunctionCheck`. Install groups the checks that name the same rule into one Trigger, whose `WhereResource` selects their resource types and whose `Controls` annotation records their control numbers, so a rule is not run on a Unit holding none of its types. Optional checks are added with `--check` or `--all`, and parameters with `--param <check>.<name>=<value>`; installing again keeps every check already installed. `guardrails update <rule> --separate <check>` moves a check into a rule of its own, to stage it ahead of the rest. `--policy-unit <space>/<unit>` installs a rule, named after the Unit, running a user's own ValidatingAdmissionPolicies from a Unit already in ConfigHub with `vet-validating-admission-policy`, so the policies checked before release are the ones enforced at admission; the Unit's bindings select the resources each policy sees. The package supplies `guardrails install` (dry run by default, `--commit` to apply) and `guardrails status`, which, given a tool's `Framework`s (a standard's controls, each mapped to the upstream controls checks name in `Corresponds`), takes `--framework` to report each of the standard's controls: the checks covering it, the rule each runs in and its stage, and the controls no check covers; A check that needs more than the resource it checks declares `OtherData`: under a key, the resource types, and optionally a `WhereResource`, it reads from the other Units on the same cluster, which its rule's Trigger selects for each Space or Target with an `OtherUnitSelection` (`Space.ReleaseTargetID = EvaluatingSpace.ReleaseTargetID` for a rule applied to Spaces, `Space.ReleaseTargetID = EvaluatingTarget.TargetID` for one applied to Targets, unless the check names another `Where`), and which a cel check reads as `other.<key>`, with `selects`, `podLabels` and `namespaceLabels` to match selectors. `guardrails annotate`, which writes a tool's findings onto each Unit as an annotation the rule then checks, is the older way to decide with more than one resource. A resource is excepted from one rule by the annotation `exceptions.guardrails.confighub.com/<rule>`, whose value says why; a check that a resource excepted from another rule cannot pass names that rule in `ExceptFrom`, and its rule honors that rule's exceptions too; `guardrails exception` adds and removes them, and lists every resource excepted, with its reason. Each rule's Trigger is created with a backing Unit, which keeps its revision history. Rules install as warnings and are made blocking with `cub trigger update <slug> --space <policy-space> --unwarn`, which a later install leaves in place. A Space that already selects Triggers its own way is reported and left alone. A Space selecting the Triggers in another Space, as a variant that `cub variant create` makes from a Space selecting its own does, and the variants made from it, is wired like a new Space when that Space holds none, and left alone when it holds some.
- **`managerkit/profiles`** -- a library of profiles: named, reusable edits stored as ConfigHub Invocations, each a function with preset arguments and optional parameters supplied at apply time. A profile that edits one kind of resource names it in `WhereResource`, which is stored with the profile, since a Unit can hold several resources and a function otherwise runs on all of them. A tool declares a `Library` of `Spec`s and gets `profile install`, `profile list` and `profile apply <profile> <space>/<unit>`, plus `fleet-edit --profile <slug>` to apply one profile to every Unit a filter selects; it refuses to run without one. Each profile's Invocation is created with a backing Unit, which keeps its revision history.
- **`managerkit/write`** -- the reporting half of a mutating command: `ParseUnitRef` for the `<space>/<unit>` argument, `Change` for the change description and dry run, and `ReportMutations`, which renders the function results per Unit, followed on a table by the diff of each Unit that changed or, on a dry run, would change, and after a commit by `ReleaseNote`: nothing reaches a Target until the Space is released.
- **`managerkit/configmaps`** -- the configmaps pack: the Mutation Triggers that roll a workload when a ConfigMap it uses changes, and bound the immutable ConfigMaps `render-configmap` leaves behind. `SetHash` runs `set-hash data binaryData` on the ConfigMaps not rendered, keeping the `confighub.com/Hash` that Needs/Provides carries to each pod template's `hash.confighub.com/<configmap>` annotation, and `Prune` runs `prune-configmaps`. Both are labeled `Pack: configmaps` (`Labels`). `cub variant upload` creates them in a Space that runs its own Triggers; a tool installs them into a policy Space for the Spaces that select their Triggers from there.

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
	Checks: []guardrails.Check{{
		Control:       "CH-1199",
		Name:          "deployment-needs-node-selector",
		Rule:          "deployment-needs-node-selector",
		Description:   "Warns on a Deployment with no nodeSelector.",
		Engine:        guardrails.EngineCEL,
		Priority:      guardrails.PriorityMedium,
		ResourceTypes: []string{"apps/v1/Deployment"},
		Failures: "has(r.spec.template.spec.nodeSelector) ? [] : [{'ResourceName': r.metadata.name, " +
			"'ResourceType': 'apps/v1/Deployment', 'Path': 'spec.template.spec.nodeSelector', " +
			"'Detail': 'the pods have no nodeSelector'}]",
	}},
}

var library = profiles.Library{
	Tool:           "cub-manage",
	Noun:           "placement profile",
	Target:         "a workload",
	Preflight:      preflight,
	InvocationName: func() string { return "cub manage" },
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

The check's rule selects Deployments with `WhereResource`, so its failures are evaluated only on Deployments and need no kind test.
