// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A differential harness over randomly generated three-way merges. The corpus in
// yamlkit_corpus_test.go covers the shapes we thought of; this covers the ones we didn't.
// Each iteration builds a base resource, makes an independent random edit on each side,
// merges, and checks the same properties the corpus checks — conservation, override
// preservation, and idempotence. Seeds are deterministic, so a failure names the case that
// produced it and can be replayed by running the one seed.

// fuzzModel is the state a generated Deployment is rendered from. Editing the model rather
// than the text is what lets an edit be described precisely enough to know whether it
// should have survived the merge.
type fuzzModel struct {
	replicas int
	image    string
	// envs is merge-keyed by name: elements have identity.
	envs []fuzzEnv
	// args is positional: elements have no identity beyond where they sit.
	args []string
	// labels is a map: keys merge independently.
	labels map[string]string
	// limits is the optional resources block; empty means absent.
	limits string
}

type fuzzEnv struct {
	name  string
	value string
}

func (m fuzzModel) render() string {
	var b strings.Builder
	b.WriteString("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: ns\n")
	if len(m.labels) > 0 {
		b.WriteString("  labels:\n")
		keys := make([]string, 0, len(m.labels))
		for key := range m.labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "    %s: %q\n", key, m.labels[key])
		}
	}
	fmt.Fprintf(&b, "spec:\n  replicas: %d\n", m.replicas)
	b.WriteString("  selector:\n    matchLabels:\n      app: web\n  template:\n    metadata:\n      labels:\n        app: web\n    spec:\n      containers:\n")
	fmt.Fprintf(&b, "      - name: app\n        image: %s\n", m.image)
	if len(m.envs) > 0 {
		b.WriteString("        env:\n")
		for _, env := range m.envs {
			fmt.Fprintf(&b, "        - name: %s\n          value: %q\n", env.name, env.value)
		}
	}
	if len(m.args) > 0 {
		b.WriteString("        args:\n")
		for _, arg := range m.args {
			fmt.Fprintf(&b, "        - %s\n", arg)
		}
	}
	if m.limits != "" {
		fmt.Fprintf(&b, "        resources:\n          limits:\n            cpu: %q\n", m.limits)
	}
	return b.String()
}

func (m fuzzModel) clone() fuzzModel {
	out := m
	out.envs = append([]fuzzEnv(nil), m.envs...)
	out.args = append([]string(nil), m.args...)
	out.labels = map[string]string{}
	for key, value := range m.labels {
		out.labels[key] = value
	}
	return out
}

func randomModel(rng *rand.Rand) fuzzModel {
	m := fuzzModel{
		replicas: 1 + rng.Intn(4),
		image:    fmt.Sprintf("app:%d.%d", rng.Intn(3), rng.Intn(9)),
		labels:   map[string]string{"tier": "web"},
	}
	for i := range 1 + rng.Intn(3) {
		m.envs = append(m.envs, fuzzEnv{name: fmt.Sprintf("VAR_%d", i), value: fmt.Sprintf("v%d", rng.Intn(5))})
	}
	for i := range rng.Intn(4) {
		m.args = append(m.args, fmt.Sprintf("--flag-%d", i))
	}
	if rng.Intn(2) == 0 {
		m.limits = fmt.Sprintf("%d", 1+rng.Intn(4))
	}
	return m
}

// fuzzEdit describes what a random edit did to positional arrays, which is what decides
// whether the resulting patch can be replayed onto its own result. See notIdempotent in
// the corpus for why: a positional element op names its element by index and nothing else.
type fuzzEdit struct {
	insertedPositional bool
	removedPositional  bool
	movedPositional    bool
}

// replayChangesResult reports whether replaying the upstream patch onto the merged result
// would change it again — the cases where positional indices cannot survive the round trip.
//
//   - The upstream removed or moved an element: on the replay the removal half of the
//     change finds an element to remove a second time — the one the first pass left in
//     place, or the one that has since taken the index.
//   - The upstream inserted an element and the downstream also changed the shape of a
//     positional array: the insertion lands at a different index than the patch recorded
//     (clamped, or shifted by the downstream's own removals), so on the replay the index
//     the patch names no longer holds the inserted element and it is inserted twice.
//
// An insertion into an array the downstream left alone lands at exactly the recorded
// index, where PatchMutations recognizes it and skips, so those stay idempotent.
func replayChangesResult(upstream, downstream fuzzEdit) bool {
	// A move is a removal and an insertion, so it carries the removal's problem: on the
	// replay the anchor finds the element the first pass already moved and removes it.
	if upstream.removedPositional || upstream.movedPositional {
		return true
	}
	return upstream.insertedPositional &&
		(downstream.insertedPositional || downstream.removedPositional || downstream.movedPositional)
}

func applyRandomEdits(rng *rand.Rand, m fuzzModel, count int) (fuzzModel, fuzzEdit) {
	out := m.clone()
	edit := fuzzEdit{}
	for range count {
		switch rng.Intn(11) {
		case 0:
			out.replicas += 1 + rng.Intn(3)
		case 1:
			out.image = fmt.Sprintf("app:%d.%d", 5+rng.Intn(3), rng.Intn(9))
		case 2: // change an existing env value
			if len(out.envs) > 0 {
				i := rng.Intn(len(out.envs))
				out.envs[i].value = fmt.Sprintf("e%d", rng.Intn(100))
			}
		case 3: // add an env var with a fresh name
			out.envs = append(out.envs, fuzzEnv{
				name:  fmt.Sprintf("NEW_%d", rng.Intn(1000)),
				value: fmt.Sprintf("n%d", rng.Intn(10)),
			})
		case 4: // remove an env var (merge-keyed: it has identity)
			if len(out.envs) > 1 {
				i := rng.Intn(len(out.envs))
				out.envs = append(out.envs[:i:i], out.envs[i+1:]...)
			}
		case 5: // insert an arg (positional)
			if len(out.args) > 0 {
				i := rng.Intn(len(out.args))
				out.args = append(out.args[:i:i], append([]string{fmt.Sprintf("--added-%d", rng.Intn(1000))}, out.args[i:]...)...)
			} else {
				out.args = append(out.args, fmt.Sprintf("--added-%d", rng.Intn(1000)))
			}
			edit.insertedPositional = true
		case 6: // remove an arg (positional)
			if len(out.args) > 0 {
				i := rng.Intn(len(out.args))
				out.args = append(out.args[:i:i], out.args[i+1:]...)
				edit.removedPositional = true
			}
		case 7: // set or clear the optional resources block
			if out.limits == "" {
				out.limits = fmt.Sprintf("%d", 5+rng.Intn(4))
			} else {
				out.limits = ""
			}
		case 8: // add or change a label
			out.labels[fmt.Sprintf("k%d", rng.Intn(4))] = fmt.Sprintf("v%d", rng.Intn(10))
		case 9: // move an arg (positional), leaving every element's content alone
			if len(out.args) > 1 {
				from := rng.Intn(len(out.args))
				to := rng.Intn(len(out.args))
				if from != to {
					arg := out.args[from]
					out.args = append(out.args[:from:from], out.args[from+1:]...)
					rest := append([]string{arg}, out.args[to:]...)
					out.args = append(out.args[:to:to], rest...)
					edit.movedPositional = true
				}
			}
		case 10: // move an env var (merge-keyed)
			if len(out.envs) > 1 {
				from := rng.Intn(len(out.envs))
				to := rng.Intn(len(out.envs))
				if from != to {
					env := out.envs[from]
					out.envs = append(out.envs[:from:from], out.envs[from+1:]...)
					rest := append([]fuzzEnv{env}, out.envs[to:]...)
					out.envs = append(out.envs[:to:to], rest...)
				}
			}
		}
	}
	return out, edit
}

func TestMergeFuzzProperties(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	const iterations = 300

	for seed := range iterations {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			baseModel := randomModel(rng)
			upstreamModel, upstreamEdit := applyRandomEdits(rng, baseModel, 1+rng.Intn(3))
			downstreamModel, downstreamEdit := applyRandomEdits(rng, baseModel, 1+rng.Intn(3))
			notIdempotent := replayChangesResult(upstreamEdit, downstreamEdit)

			base, upstream, downstream := baseModel.render(), upstreamModel.render(), downstreamModel.render()

			patch, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, upstream), 1, provider)
			require.NoErrorf(t, err, "base:\n%s\nupstream:\n%s", base, upstream)
			targetDiff, err := yamlkit.ComputeMutations(parseCorpus(t, base), parseCorpus(t, downstream), 2, provider)
			require.NoError(t, err)

			merged, conflicts, err := yamlkit.PatchMutations(parseCorpus(t, downstream), nil, patch, nil, provider, nil)
			require.NoError(t, err)
			checkMergeProperties(t, describeFuzzCase("subtraction off", base, upstream, downstream, merged),
				provider, patch, targetDiff, parseCorpus(t, upstream), parseCorpus(t, downstream),
				merged, conflicts, false, notIdempotent)

			mergedSub, conflictsSub, err := yamlkit.PatchMutations(parseCorpus(t, downstream), nil, patch, targetDiff, provider, nil)
			require.NoError(t, err)
			checkMergeProperties(t, describeFuzzCase("subtraction on", base, upstream, downstream, mergedSub),
				provider, patch, targetDiff, parseCorpus(t, upstream), parseCorpus(t, downstream),
				mergedSub, conflictsSub, true, notIdempotent)
		})
	}
}

// describeFuzzCase renders the whole generated case into the mode label, so a failure
// carries the inputs that produced it rather than only a path.
func describeFuzzCase(mode, base, upstream, downstream string, merged gaby.Container) string {
	return fmt.Sprintf("%s\n--- base ---\n%s--- upstream ---\n%s--- downstream ---\n%s--- merged ---\n%s",
		mode, base, upstream, downstream, merged.String())
}
