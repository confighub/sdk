// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/constants"
)

// pagedServer serves total entities, the numbers 0 to total-1, a page at a time. Its continue
// token is the next number, and it returns at most maxPage entities however many are asked for,
// as a server whose filters drop entities after reading them can.
type pagedServer struct {
	total    int
	maxPage  int
	requests []*int
}

func (s *pagedServer) read(limit *int, token *string) (*http.Response, *[]int, error) {
	s.requests = append(s.requests, limit)
	start := 0
	if token != nil {
		start, _ = strconv.Atoi(*token)
	}
	end := s.total
	if limit != nil {
		end = min(start+min(*limit, s.maxPage), s.total)
	}
	page := []int{}
	for i := start; i < end; i++ {
		page = append(page, i)
	}
	res := &http.Response{Header: http.Header{}}
	if limit != nil && end < s.total {
		res.Header.Set(constants.ContinueHeader, strconv.Itoa(end))
	}
	return res, &page, nil
}

func values(items []*int) []int {
	out := make([]int, len(items))
	for i, item := range items {
		out[i] = *item
	}
	return out
}

func TestReadPages(t *testing.T) {
	t.Run("WithoutLimitOneRequest", func(t *testing.T) {
		server := &pagedServer{total: 2500, maxPage: 1000}
		items, err := ReadPages(ListOpts{}, server.read)
		require.NoError(t, err)
		assert.Len(t, items, 2500)
		require.Len(t, server.requests, 1)
		assert.Nil(t, server.requests[0], "no limit is sent, so the server returns every entity")
	})

	t.Run("FollowsTokensToTheLimit", func(t *testing.T) {
		server := &pagedServer{total: 2500, maxPage: 1000}
		items, err := ReadPages(ListOpts{Limit: 1500}, server.read)
		require.NoError(t, err)
		assert.Equal(t, 1500, len(items))
		assert.Equal(t, 0, *items[0])
		assert.Equal(t, 1499, *items[1499])
		require.Len(t, server.requests, 2)
		assert.Equal(t, MaxListPageSize, *server.requests[0])
		assert.Equal(t, 500, *server.requests[1], "the last page asks only for what is still missing")
	})

	t.Run("ShortPagesAreNotTheEnd", func(t *testing.T) {
		server := &pagedServer{total: 10, maxPage: 3}
		items, err := ReadPages(ListOpts{Limit: 100}, server.read)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, values(items))
		assert.Len(t, server.requests, 4)
	})

	t.Run("NoEntitiesIsEmptyNotNil", func(t *testing.T) {
		server := &pagedServer{total: 0, maxPage: 1000}
		for _, opts := range []ListOpts{{}, {Limit: 10}} {
			items, err := ReadPages(opts, server.read)
			require.NoError(t, err)
			assert.NotNil(t, items, "a caller that encodes the list as JSON gets [], not null")
			assert.Empty(t, items)
		}
	})

	t.Run("StopsWithoutAToken", func(t *testing.T) {
		server := &pagedServer{total: 5, maxPage: 1000}
		items, err := ReadPages(ListOpts{Limit: 100}, server.read)
		require.NoError(t, err)
		assert.Len(t, items, 5)
		assert.Len(t, server.requests, 1)
	})
}
