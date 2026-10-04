// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/constants"
)

// maxBulkDeleteSweeps bounds how many times a paged bulk delete is repeated. See
// bulkPagingTransport.
const maxBulkDeleteSweeps = 10

// bulkCollectionPath matches the path of an organization-level bulk operation, /api/<entity>.
var bulkCollectionPath = regexp.MustCompile(`/api/[a-z_]+$`)

// bulkPagingTransport sends a bulk patch, create or delete in pages and answers with one response
// for all of them, so that a caller written for a single request works unchanged however many
// entities the operation selects. Each page asks for at most [MaxListPageSize] entities, and the
// server also stops a page when it runs short of time, so that no request outlives its timeout.
//
// The pages' per-entity results are concatenated, and the status is 200 when every entity
// succeeded and 207 otherwise. A page that failed as a whole, which the server answers with one
// error rather than a result per entity, contributes one result holding that error, unless it was
// the only page, whose response is returned as it was.
//
// A delete is told what else its request deletes, so that a reference from another entity being
// deleted does not block it. Across pages, that reference may be in a later page, so a delete can
// fail as still referenced only to be deletable once the later page has gone. When a sweep through
// the pages deleted something and something failed, the transport sweeps the selection again, up to
// maxBulkDeleteSweeps times; what was deleted no longer matches it. The results are the deletes of
// every sweep and the failures of the last.
//
// An endpoint that does not page ignores limit and answers in one response with no continue token,
// and so does a server that predates paging, so the transport is safe to apply to every bulk
// request.
type bulkPagingTransport struct {
	base http.RoundTripper
}

func (t *bulkPagingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isPageableBulkRequest(req) {
		return t.base.RoundTrip(req)
	}
	var deleted []json.RawMessage
	for sweep := 1; ; sweep++ {
		results, pageErrors, last, err := t.sweep(req)
		if err != nil {
			return nil, err
		}
		if results == nil && len(pageErrors) == 0 {
			if sweep == 1 {
				// The request was refused before acting on anything; answer with the refusal.
				return last, nil
			}
			body, err := io.ReadAll(last.Body)
			if err != nil {
				return nil, err
			}
			pageErrors = []json.RawMessage{bulkPageError(body)}
		}
		succeeded, failed := splitBulkResults(results)
		failed = append(failed, pageErrors...)
		deleted = append(deleted, succeeded...)
		if req.Method != http.MethodDelete || len(failed) == 0 || len(succeeded) == 0 || sweep == maxBulkDeleteSweeps {
			return mergedBulkResponse(req, last, deleted, failed)
		}
	}
}

// isPageableBulkRequest reports whether req is an organization-level bulk patch, create or delete
// that selects entities with where or filter, does not page itself, and can be sent again.
func isPageableBulkRequest(req *http.Request) bool {
	switch req.Method {
	case http.MethodPatch, http.MethodPost, http.MethodDelete:
	default:
		return false
	}
	query := req.URL.Query()
	if query.Get("where") == "" && query.Get("filter") == "" {
		return false
	}
	if query.Has("limit") || query.Has("continue") {
		return false
	}
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return false
	}
	return bulkCollectionPath.MatchString(req.URL.Path)
}

// sweep sends req page by page until the server returns no continue token. It returns the
// per-entity results, one result for each page that failed as a whole after the first, and the last
// response. A first page that fails as a whole with no token is returned as last, with no results.
func (t *bulkPagingTransport) sweep(req *http.Request) (results []json.RawMessage, pageErrors []json.RawMessage, last *http.Response, err error) {
	token := ""
	for page := 0; ; page++ {
		res, err := t.base.RoundTrip(pageRequest(req, token))
		if err != nil {
			return nil, nil, nil, err
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, nil, nil, err
		}
		res.Body = io.NopCloser(bytes.NewReader(body))
		last = res
		token = res.Header.Get(constants.ContinueHeader)

		switch res.StatusCode {
		case http.StatusOK, http.StatusMultiStatus:
			var pageResults []json.RawMessage
			if err := json.Unmarshal(body, &pageResults); err != nil {
				// Not a bulk response: hand it back as it came.
				return nil, nil, res, nil
			}
			results = append(results, pageResults...)
		default:
			if page == 0 && token == "" {
				return nil, nil, res, nil
			}
			pageErrors = append(pageErrors, bulkPageError(body))
		}
		if token == "" {
			if results == nil {
				results = []json.RawMessage{}
			}
			return results, pageErrors, last, nil
		}
	}
}

// pageRequest is req with limit, and with continue when token is set, and a fresh copy of its body.
func pageRequest(req *http.Request, token string) *http.Request {
	page := req.Clone(req.Context())
	query := page.URL.Query()
	query.Set("limit", strconv.Itoa(MaxListPageSize))
	if token != "" {
		query.Set("continue", token)
	}
	page.URL.RawQuery = query.Encode()
	if req.GetBody != nil {
		page.Body, _ = req.GetBody()
	}
	return page
}

// bulkPageError is the result standing for a page that failed as a whole.
func bulkPageError(body []byte) json.RawMessage {
	if !json.Valid(body) {
		body, _ = json.Marshal(map[string]string{"Message": strings.TrimSpace(string(body))})
	}
	result, _ := json.Marshal(map[string]json.RawMessage{"Error": body})
	return result
}

// splitBulkResults separates the results of entities a bulk operation acted on from those it
// failed on, which carry an Error.
func splitBulkResults(results []json.RawMessage) (succeeded, failed []json.RawMessage) {
	for _, result := range results {
		var withError struct {
			Error json.RawMessage
		}
		if json.Unmarshal(result, &withError) == nil && len(withError.Error) > 0 && string(withError.Error) != "null" {
			failed = append(failed, result)
		} else {
			succeeded = append(succeeded, result)
		}
	}
	return succeeded, failed
}

func mergedBulkResponse(req *http.Request, last *http.Response, succeeded, failed []json.RawMessage) (*http.Response, error) {
	results := append(append([]json.RawMessage{}, succeeded...), failed...)
	body, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	status := http.StatusOK
	if len(failed) > 0 {
		status = http.StatusMultiStatus
	}
	header := last.Header.Clone()
	header.Del(constants.ContinueHeader)
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	return &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Proto:         last.Proto,
		ProtoMajor:    last.ProtoMajor,
		ProtoMinor:    last.ProtoMinor,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
