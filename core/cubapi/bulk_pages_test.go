// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/constants"
)

// fakeBulkServer acts on the entities 0 to total-1 that are still there, perPage at a time,
// returning one result per entity and a continue token while more remain. failOnce makes an
// entity fail the first time it is acted on, as a delete blocked by a reference does.
type fakeBulkServer struct {
	total    int
	perPage  int
	failOnce map[int]bool
	gone     map[int]bool
	bodies   []string
	queries  []string
}

func (s *fakeBulkServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, string(body))
	s.queries = append(s.queries, r.URL.RawQuery)
	after := -1
	if token := r.URL.Query().Get("continue"); token != "" {
		after, _ = strconv.Atoi(token)
	}
	var results []map[string]any
	last := after
	status := http.StatusOK
	for i := after + 1; i < s.total && len(results) < s.perPage; i++ {
		last = i
		if s.gone[i] {
			continue
		}
		if s.failOnce[i] {
			delete(s.failOnce, i)
			results = append(results, map[string]any{"Error": map[string]string{"Message": "still referenced"}})
			status = http.StatusMultiStatus
			continue
		}
		s.gone[i] = true
		results = append(results, map[string]any{"Message": fmt.Sprintf("%d deleted", i)})
	}
	if last < s.total-1 {
		w.Header().Set(constants.ContinueHeader, strconv.Itoa(last))
	}
	if results == nil {
		results = []map[string]any{}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(results)
}

func sendBulk(t *testing.T, server *httptest.Server, method, query, body string) (int, []map[string]any) {
	t.Helper()
	client := &http.Client{Transport: &bulkPagingTransport{base: http.DefaultTransport}}
	req, err := http.NewRequest(method, server.URL+"/api/tag?"+query, bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	var results []map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&results))
	return res.StatusCode, results
}

func TestBulkPagingTransport(t *testing.T) {
	t.Run("FollowsTokensAndResendsTheBody", func(t *testing.T) {
		fake := &fakeBulkServer{total: 25, perPage: 10, gone: map[int]bool{}}
		server := httptest.NewServer(fake)
		defer server.Close()
		status, results := sendBulk(t, server, http.MethodPatch, "where=x", `{"DisplayName":"p"}`)
		assert.Equal(t, http.StatusOK, status)
		assert.Len(t, results, 25)
		require.Len(t, fake.bodies, 3)
		for _, body := range fake.bodies {
			assert.Equal(t, `{"DisplayName":"p"}`, body, "every page carries the patch")
		}
		assert.Contains(t, fake.queries[0], "limit=1000")
		assert.NotContains(t, fake.queries[0], "continue=")
		assert.Contains(t, fake.queries[1], "continue=9")
	})

	t.Run("DeleteSweepsAgainAfterProgress", func(t *testing.T) {
		fake := &fakeBulkServer{total: 25, perPage: 10, gone: map[int]bool{}, failOnce: map[int]bool{3: true, 17: true}}
		server := httptest.NewServer(fake)
		defer server.Close()
		status, results := sendBulk(t, server, http.MethodDelete, "where=x", "")
		assert.Equal(t, http.StatusOK, status, "the second sweep deleted what the first could not")
		assert.Len(t, results, 25)
		for _, result := range results {
			assert.NotContains(t, result, "Error")
		}
	})

	t.Run("PatchDoesNotSweepAgain", func(t *testing.T) {
		fake := &fakeBulkServer{total: 5, perPage: 10, gone: map[int]bool{}, failOnce: map[int]bool{2: true}}
		server := httptest.NewServer(fake)
		defer server.Close()
		status, results := sendBulk(t, server, http.MethodPatch, "where=x", "{}")
		assert.Equal(t, http.StatusMultiStatus, status)
		assert.Len(t, results, 5)
		assert.Len(t, fake.queries, 1)
	})

	t.Run("RequestsThatDoNotPageAreUntouched", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/api/tag?where=x"},
			{http.MethodPatch, "/api/tag?where=x&limit=5"},
			{http.MethodDelete, "/api/space/abc/tag/def"},
			{http.MethodPost, "/api/space"},
		} {
			req, err := http.NewRequest(tc.method, "http://example.invalid"+tc.path, strings.NewReader("{}"))
			require.NoError(t, err)
			assert.False(t, isPageableBulkRequest(req), "%s %s", tc.method, tc.path)
		}
	})
}
