// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"regexp"
	"strings"
)

var (
	ansiRE       = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	mutationLine = regexp.MustCompile(`^\s*[+~-]\s*\[(?:Add|Update|Delete)\]\s`)
	resourceLine = regexp.MustCompile(`^Resource:\s`)
	// bookkeepingRE matches a body line whose payload starts with a
	// well-known bookkeeping key. Used to drop Add / Update mutations whose
	// only content is bookkeeping cub injects on every merge-external-source
	// apply.
	//
	// Two flavors are recognized:
	//   - "confighub.com/..." (Kubernetes/YAML annotation keys)
	//   - "configHub.resourceMergeID" (AppConfig flat-key bookkeeping;
	//     "configHub.<other>" can be legitimate schema config a package
	//     author declared — e.g., configHub.configName / configHub.configSchema
	//     — so the AppConfig match is keyed on the specific bookkeeping name,
	//     not the configHub prefix in general).
	bookkeepingRE = regexp.MustCompile(`^\s*(?:confighub\.com/|configHub\.resourceMergeID(?:[\s=:]|$))`)
	// deleteOnBookkeepingHeader matches a mutation header line for a [Delete]
	// on the bookkeeping key. The body of a Delete mutation holds only the
	// deleted value (a UUID), so the body-based bookkeepingRE check can't
	// catch it — the signal is in the path. Two formats:
	//   - Kubernetes annotation, "." as separator, "~1" as the escape for
	//     "." inside the key:
	//       [Delete] metadata.annotations.confighub~1com/ResourceMergeID
	//     (literal "." form is also accepted, defensively).
	//   - AppConfig flat-key form:
	//       [Delete] configHub.resourceMergeID
	deleteOnBookkeepingHeader = regexp.MustCompile(`\[Delete\]\s+(?:.*\.annotations\.confighub(?:~1com|\.com)/|configHub\.resourceMergeID(?:\s|$))`)
)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// isNoChange reports whether a cleaned/filtered mutations diff represents no
// effective change.
func isNoChange(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || strings.Contains(t, "No new changes") {
		return true
	}
	// After bookkeeping filtering, the diff may consist of only the "New
	// changes from update from <path>:" preamble with no Resource: blocks.
	// Treat that as no change too.
	if !strings.Contains(t, "Resource:") {
		return true
	}
	return false
}

// filterBookkeepingMutations walks the cub -o mutations output and drops any
// mutation whose body contains only confighub.com/* keys (currently just
// ResourceMergeID). Resources whose mutations are all dropped are removed too.
// Format is parsed line-by-line:
//
//	Resource: <type> <name>            <- resource header
//	  ~ [Update] <path>  (#N)          <- mutation header
//	    <content lines, indented more> <- mutation body
//
// Robust against the New-changes prefix and the resource-no-changes case.
// Conservative: when in doubt, keeps the mutation.
//
// Without this filter a repeated reconcile does not converge on its second
// run, because the new file body lacks the ResourceMergeID annotation cub
// injected on the prior merge.
func filterBookkeepingMutations(in string) string {
	lines := strings.Split(in, "\n")
	type block struct {
		header  string
		body    []string
		dropped bool
	}
	var (
		out        []string
		currentRes []string
		blocks     []block
		flushRes   func()
	)
	flushBlocks := func() {
		for _, b := range blocks {
			if b.dropped {
				continue
			}
			currentRes = append(currentRes, b.header)
			currentRes = append(currentRes, b.body...)
		}
		blocks = nil
	}
	flushRes = func() {
		flushBlocks()
		// Only emit the resource header if at least one mutation survived.
		// currentRes[0] is the "Resource:" header line.
		if len(currentRes) > 1 {
			out = append(out, currentRes...)
		}
		currentRes = nil
	}
	var pendingBlock *block
	closePending := func() {
		if pendingBlock == nil {
			return
		}
		// Two flavors of "bookkeeping-only" mutations to drop:
		//
		// 1. [Add] / [Update] on the annotation: body holds the
		//    confighub.com/<key>: <value> pair. Caught by the body-based
		//    check below.
		// 2. [Delete] on the annotation: body holds only the deleted value (a
		//    UUID). The signal lives in the header path — caught by
		//    deleteOnBookkeepingHeader.
		//
		// Empty-body mutations (like "+ [Add] metadata.labels.foo") are real
		// changes — the path in the header is the diff — and must not be dropped.
		if deleteOnBookkeepingHeader.MatchString(pendingBlock.header) {
			pendingBlock.dropped = true
			blocks = append(blocks, *pendingBlock)
			pendingBlock = nil
			return
		}
		bodyHasContent := false
		bodyAllBookkeeping := true
		for _, l := range pendingBlock.body {
			t := strings.TrimSpace(l)
			if t == "" {
				continue
			}
			bodyHasContent = true
			if !bookkeepingRE.MatchString(l) {
				bodyAllBookkeeping = false
				break
			}
		}
		pendingBlock.dropped = bodyHasContent && bodyAllBookkeeping
		blocks = append(blocks, *pendingBlock)
		pendingBlock = nil
	}
	for _, line := range lines {
		switch {
		case resourceLine.MatchString(line):
			closePending()
			flushRes()
			currentRes = []string{line}
		case mutationLine.MatchString(line):
			closePending()
			pendingBlock = &block{header: line}
		case pendingBlock != nil:
			pendingBlock.body = append(pendingBlock.body, line)
		default:
			out = append(out, line)
		}
	}
	closePending()
	flushRes()
	return strings.Join(out, "\n")
}
