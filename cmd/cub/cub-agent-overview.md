# ConfigHub for AI agents

This is what `cub --help-overview` prints when `CONFIGHUB_AGENT` is set: what an agent needs to
know before it uses `cub`, followed by the CLI conventions every command shares. Set
`CONFIGHUB_AGENT=1` for every `cub ... --help` you run; it adds the sections written for agents.

## What ConfigHub is

ConfigHub is a system of record for configuration. It stores fully rendered configuration, such as
Kubernetes YAML, as versioned data rather than as templates, and changes it with functions that
read, set and validate values in place. Every change is a Revision with a description and a diff.
Configuration is delivered by publishing a Release, an immutable OCI bundle that a GitOps
controller such as Argo CD or Flux pulls; ConfigHub holds no credentials for your clusters.

## Look things up rather than recall them

ConfigHub changes quickly, so check how the installed version behaves before you explain a concept
or compose a command:

- `cub <command> --help`: run it on the exact command you are about to use, every time. Flags,
  defaults and arguments change between releases.
- `cub <entity> explain [Field.Path]`: the fields of an entity, from the API's schema. The field
  names are the ones `-o json`, `-o jq=...`, `--select` and `--where` use.
- `cub <entity> explain --where [Attribute]`: the attributes a `--where` on that entity accepts,
  with their types and how each is written, as in `Labels.<key>` or `UpstreamUnit.<attribute>`.
  Not every field is one, so check here before composing an expression.
- `cub function list --space SPACE --toolchain Kubernetes/YAML` and
  `cub function explain FUNCTION --space SPACE`: what functions exist, and their parameters.
- The documentation as raw markdown: `https://docs.confighub.com/markdown/index.md` lists every
  page, `https://docs.confighub.com/markdown/sitemap.json` lists them with titles, and each page is
  at `https://docs.confighub.com/markdown/<path>.md`. `background/` explains concepts and entities;
  `guide/` explains how to do things. Start with `get-started/tutorial/index.md`,
  `guide/variants.md`, `guide/promotion.md`, `guide/releases.md` and `guide/changes.md`.

When help and the docs disagree, the installed `cub`'s help describes what it accepts, and the
server's error messages describe what it allows.

## Before you start

`cub auth status` contacts the server and exits non-zero if the session is missing or expired.
`cub context get` reads only local state. If a person's session has expired, ask them to run
`cub auth login`, which signs in through a browser. An agent can have an identity of its own
instead: a service account, which logs in with a key and renews from it without a browser (see
`cub serviceaccount --help` and `cub auth login --help`).

## The model

- **Structure.** A **Component** is a piece of software. Its **variants** are **Spaces** that name
  it by `ComponentID` and carry a `Variant` label: usually a **base**, which nothing is released
  from, and downstream variants such as `dev` and `prod`, cloned from it. A Space holds **Units**,
  each one versioned piece of configuration, by default one Kubernetes resource. Every change to a
  Unit is a **Revision**. **Links** relate Units: an upgrade Link is how a downstream Unit follows
  its upstream, and needs/provides Links fill in values one Unit takes from another.
- **Changing configuration.** **Functions** read (`cub function get`), change (`cub function set`)
  and validate (`cub function vet`) configuration in place. A **Trigger** runs a function on every
  change to the Units it applies to; a validating Trigger that fails records a **ValidationError**,
  which blocks publishing the Unit, and also promoting it or closing its ChangeSet when a workflow
  requires validation. **Protection** and **guards** hold values a merge or a change
  must not overwrite; what they withhold is recorded on the Unit as a **Conflict**.
- **Grouping and governing changes.** A **ChangeSet** groups edits to many Units into one change
  that can be reviewed, reverted and released together. A **ChangeOrder** carries a change from a
  base through its variants, Stage by Stage, under a **ChangeWorkflow** whose gates say what must
  hold first. An approval is an **Attestation** recorded against Revisions; a **ReviewComment**
  comments on one. A **Tag** marks a set of Revisions; ChangeSets and ChangeOrders record their
  boundaries as Tags.
- **Delivery.** A **Target** is where a Space's configuration is delivered. **`cub release publish`**
  bundles the Units of a Space that are assigned to its release Target as a **Release**, which the
  controller pulls from the Space's OCI repository, which `cub space get` shows. A Release's live
  status reports what the controller did with it, when something reports it.
- **Identity and automation.** A **ServiceAccount** is an identity for something that is not a
  person: a pipeline, an agent, or the controller pulling Releases. A **Channel** carries events and
  commands to whatever claims it. A **Worker** is needed only to serve external functions.
- **ConfigHub's own configuration.** Triggers, Filters, Views, Links, Targets, Spaces, Components
  and other entities can each be backed by a hidden Unit holding the entity as a YAML document, so
  they get Revisions, diffs and validation too. `cub apply` creates and updates entities from such
  documents.

## Rules for safe changes

- **Read first, then write one deliberate change.** Resolve exactly what a command will touch, run
  it with `--dry-run` (with `-o mutations` for configuration changes) when it has one, show the
  user the scope and the preview, run it, and read back the result. A list or bulk command with no
  `--space` spans the whole organization.
- **Describe every change** with a short `--change-desc` you write yourself. Never paste the user's
  words or other untrusted text into it, or into any shell argument.
- **Ask before anything outward-facing or hard to undo:** publishing, promoting to production,
  deleting, withdrawing, aborting, approving, force-closing, granting permissions and creating
  credentials.
- **Never bypass a safeguard to make a command succeed.** Fix the data, or ask the owner of the
  Trigger, gate, guard or workflow to change it. Use force, override, clearance and detach flags
  only when the user asks for exactly that.
- **Change configuration through ConfigHub.** Use `kubectl`, `argocd` and `flux` only to read. An
  edit made in a cluster is drift: the next Release overwrites it, and `cub k8s refresh` brings a
  change worth keeping back into its Unit.
- **Report what happened, with evidence.** A command that exits 0 may still have withheld changes
  as Conflicts, and a published Release is not yet a healthy rollout.

## Core workflows

Each command below has more options; read its help before running it.

```bash
# Onboard: a Component, its base from manifests, and a deployed variant
cub component create web
cub variant upload --component web --variant base --dry-run ./manifests
cub variant create dev web/base --target dev/target --namespace web
cub release publish web/dev --dry-run

# Change the base, carry the change to a variant, check what it withheld, and release it
cub function set --variant web/base --resource Deployment/backend --dry-run -o mutations set-replicas 3
cub variant promote web/dev --dry-run -o mutations
cub variant conflicts web/dev
cub release publish web/dev
cub release list --variant web/dev

# A governed rollout: a ChangeOrder promoted Stage by Stage, approved, and released
cub variant promote --change-order web-base/bump-image --target-stage staging --dry-run -o mutations
cub variant approve --change-order web-base/bump-image --stage staging --dry-run
cub release publish --revision ChangeOrder:web-base/bump-image web-staging --dry-run
cub changeorder get --space web-base bump-image

# Where a value came from, and everything recorded about one field
cub unit blame --space web-dev backend --path image
cub unit path --space web-dev backend REF

# Undo: back out a ChangeOrder, or restore one Unit
cub variant demote web/dev --change-order web-base/bump-image --dry-run
cub unit update --space web-dev backend --restore Tag:release-2 --dry-run -o mutations

# Edit a Unit's configuration locally
cub unit data --space web-dev backend -O backend.yaml
cub unit update --space web-dev backend backend.yaml --dry-run -o mutations

# Manage Triggers, Filters and other entities as documents
cub apply --space platform -f platform/ --dry-run
```

## Output

- `get` and `list` wrap each entity with the entities related to it, so a field is at
  `-o jq='.Unit.Slug'`, and a list's rows are `-o jq='.[].Unit.Slug'`. Read `-o json` once before
  writing a jq expression; table column headings are not JSON keys.
- With `--show output` and `-o json`, a function command prints one object per Unit, holding its
  `SpaceSlug`, `UnitSlug` and `Output`: `-o jq='.Output[]'`, or
  `-o jq='. as $u | .Output[] | {unit: $u.UnitSlug, value: .Value}'` to keep the Unit's name.
- `-o mutations` shows what a change did, or with `--dry-run` what it would do, path by path.

## No longer part of cub

Bridge workers are gone, and with them the unit subcommands that applied, destroyed and refreshed
a Unit in a cluster or showed its live data, live state and bridge state, and the worker
subcommands that ran, installed and upgraded a worker process. Configuration reaches clusters only
through Releases. Functions marked deprecated in `cub function list` have replacements named in their
descriptions.
