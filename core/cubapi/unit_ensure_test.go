// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// ensureServer serves a Space holding the Unit lib when exists, whose configuration an upload of
// data would change when changes. It records each write: create, patch, dry-run and upload.
func ensureServer(t *testing.T, exists, changes bool) (*Client, *[]string) {
	t.Helper()
	spaceID, unitID := uuid.NewString(), uuid.NewString()
	unit := fmt.Sprintf(`{"UnitID":%q,"SpaceID":%q,"Slug":"lib"}`, unitID, spaceID)
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/unit"):
			if exists {
				_, _ = fmt.Fprintf(w, `[{"Unit":%s}]`, unit)
			} else {
				_, _ = w.Write([]byte("[]"))
			}
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/unit"):
			writes = append(writes, "create")
			_, _ = fmt.Fprintf(w, `{"Unit":%s}`, unit)
		case r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			writes = append(writes, "patch "+string(body))
			_, _ = fmt.Fprintf(w, `{"Unit":%s}`, unit)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/data"):
			if r.URL.Query().Get("dry_run") == "true" {
				writes = append(writes, "dry-run")
				diff := `{}`
				if changes {
					diff = `{"Resources":[{"ResourceName":"p"}]}`
				}
				_, _ = fmt.Fprintf(w, `{"Unit":%s,"Diff":%s}`, unit, diff)
				return
			}
			writes = append(writes, "upload "+r.URL.Query().Get("last_change_description"))
			_, _ = fmt.Fprintf(w, `{"Unit":%s}`, unit)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &writes
}

// EnsureUnitData creates a missing Unit with its data, replaces an existing one's only when told
// to and only when the data changes it, and leaves one it is not told to replace alone.
func TestEnsureUnitData(t *testing.T) {
	annotations := map[string]string{"Source": "example@abc"}
	for name, tc := range map[string]struct {
		exists, changes, replace bool
		want                     []string
	}{
		"missing":             {false, false, true, []string{"create", "upload install"}},
		"missing, kept":       {false, false, false, []string{"create", "upload install"}},
		"kept":                {true, true, false, nil},
		"replaced, unchanged": {true, false, true, []string{`patch {"Annotations":{"Source":"example@abc"}}`, "dry-run"}},
		"replaced":            {true, true, true, []string{`patch {"Annotations":{"Source":"example@abc"}}`, "dry-run", "upload install"}},
	} {
		c, writes := ensureServer(t, tc.exists, tc.changes)
		u, err := EnsureUnitData(context.Background(), c, goclientnew.Unit{
			SpaceID: goclientnew.UUID(uuid.New()), Slug: "lib", ToolchainType: "Kubernetes/YAML", Annotations: annotations,
		}, "kind: ConfigMap\n", "install", tc.replace)
		if err != nil || u == nil || u.Slug != "lib" {
			t.Errorf("%s: unit %v, err %v", name, u, err)
		}
		if !slices.Equal(*writes, tc.want) {
			t.Errorf("%s: writes = %q, want %q", name, *writes, tc.want)
		}
	}
}
