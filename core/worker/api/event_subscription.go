// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ConfigHub's event-log vocabulary (the EventType values, such as
// "apply.completed" and "release.published") is open-ended and owned by the
// server, so the SDK deliberately does not enumerate it — mirroring the API
// spec, where EventType is an unconstrained string. A bot names the event types
// it cares about itself, the same way it would name one the SDK has never heard
// of.

// EventSubscription is a worker's standing request to receive facts from
// ConfigHub's event log over the long-poll connection. The worker declares its
// subscriptions in WorkerInfo at connect time; the server matches the log's
// scoping columns against the filter and pushes matching events down the same
// connection it already holds open.
//
// A subscription is a filter, not a queue: ConfigHub keeps no per-message
// delivery state. It keeps one cursor per (worker, Name) — the bookmark — so a
// restarted worker resumes where it left off instead of replaying the log or
// skipping to the present.
type EventSubscription struct {
	// Name is the stable per-worker subscription name. The server keys the
	// delivery cursor on it, so it must be identical across reconnects for
	// resume to work. A worker with a single subscription uses a constant.
	Name string `description:"Stable per-worker subscription name; the server keys the delivery cursor on it, so it must be identical across reconnects."`

	// EventTypes are the event types to receive, for example "apply.completed"
	// or "release.published". Empty means every type.
	EventTypes []string `json:",omitempty" description:"Event types to receive, e.g. \"apply.completed\", \"release.published\". Empty means every type."`

	// SpaceID optionally scopes delivery to one Space (UUID string). Empty means
	// any Space.
	SpaceID string `json:",omitempty" description:"Optional Space scoping filter (UUID). Empty means any Space."`

	// TargetID optionally scopes delivery to one Target (UUID string). Empty
	// means any Target.
	TargetID string `json:",omitempty" description:"Optional Target scoping filter (UUID). Empty means any Target."`

	// FromCursor optionally overrides where delivery begins. When set, delivery
	// starts after this cursor; when unset, delivery resumes from the
	// server-stored bookmark, or from the log tail on a first-ever connect.
	FromCursor *int64 `json:",omitempty" description:"Optional starting cursor. When set, delivery begins after it; when unset, resumes from the stored bookmark, or the log tail on first connect."`
}

// EventLogEntry is one delivered event: a fact from ConfigHub's event log,
// addressed to no one. It is the Data of an EventMessage whose Event is
// api.EventLog. A bot reacts to it however it likes; the reaction is the bot's
// own business and never came from ConfigHub.
type EventLogEntry struct {
	// CursorID is this event's monotonic position in the log. A consumer that
	// has handled through here resumes after it.
	CursorID int64 `description:"Monotonic cursor of this event in the log."`

	// EventType is the fact's type, for example "apply.completed".
	EventType string `description:"The event type."`

	// SubjectEntityType is the entity type the event is about, for example
	// "Unit" or "Release".
	SubjectEntityType string `description:"Entity type the event is about."`

	// SubjectEntityID is the id of the entity the event is about.
	SubjectEntityID uuid.UUID `description:"Id of the entity the event is about."`

	// SpaceID is the event's Space scope, if any (UUID string).
	SpaceID string `json:",omitempty" description:"Space scope, if any."`

	// TargetID is the event's Target scope, if any (UUID string).
	TargetID string `json:",omitempty" description:"Target scope, if any."`

	// Payload is the event-type-specific data, opaque to the transport.
	Payload json.RawMessage `json:",omitempty" description:"Event-type-specific payload."`

	// CreatedAt is when the event was emitted.
	CreatedAt time.Time `description:"When the event was emitted."`

	// SubscriptionName is the subscription this event was delivered for.
	SubscriptionName string `description:"The subscription this event was delivered for."`
}
