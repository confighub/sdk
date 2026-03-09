// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"bytes"

	"github.com/confighub/sdk/bridge-worker/api"
)

type StatusWriter struct {
	wctx       api.BridgeWorkerContext
	action     api.ActionType
	buffer     bytes.Buffer
	dirty      bool
	maxBufSize int
}

func NewStatusWriter(wctx api.BridgeWorkerContext, action api.ActionType) *StatusWriter {
	return &StatusWriter{
		wctx:       wctx,
		action:     action,
		maxBufSize: 4096,
	}
}

func (w *StatusWriter) Write(p []byte) (n int, err error) {
	n, err = w.buffer.Write(p)
	if err != nil {
		return n, err
	}
	w.dirty = true

	if w.buffer.Len() >= w.maxBufSize {
		if err := w.Flush(); err != nil {
			return n, err
		}
	}

	return n, nil
}

func (w *StatusWriter) Flush() error {
	if !w.dirty {
		return nil
	}

	if w.buffer.Len() > 0 {
		if err := w.wctx.SendStatus(&api.ActionResult{
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:  w.action,
				Status:  api.ActionStatusProgressing,
				Result:  api.ActionResultNone,
				Message: w.buffer.String(),
			},
		}); err != nil {
			return err
		}
		w.buffer.Reset()
		w.dirty = false
	}

	return nil
}
