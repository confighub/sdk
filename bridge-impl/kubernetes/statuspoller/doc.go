// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package statuspoller augments sigs.k8s.io/cli-utils/kstatus with a Stuck
// state driven by per-kind classifiers. It wraps a kstatus StatusPoller and
// emits the classifier-enhanced Event stream.
//
// Design:
//   - Classifiers are pure(ish) functions keyed by GroupKind.
//   - On each upstream kstatus event, we reclassify the resource.
//   - A periodic ticker reclassifies every resource even when kstatus is
//     silent — this is what allows Stuck transitions based on elapsed time
//     (e.g., Deployment with ImagePullBackOff) to be observed.
//   - Events are emitted only when the augmented status actually changes.
//
// Liveness: the poller never "squats" silently — whenever a classifier would
// return Stuck, the poller emits Stuck within one re-evaluation interval.
package statuspoller
