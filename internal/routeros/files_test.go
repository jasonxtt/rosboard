package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestTypedDirectoryCommandsUseFixedRESTPathsAndPreserveNames(t *testing.T) {
	var paths []string
	var bodies []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error(r.Method)
		}
		paths = append(paths, r.URL.Path)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	c := NewMutationClient(server.URL, "test", "private-test-password")
	ctx := context.Background()
	if err := c.CreateDirectory(ctx, "/sata1/中文 空格"); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameDirectory(ctx, "**AbCd0123zZ_LONG-opaque", "/sata1/重命名 空格"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDirectory(ctx, "**EfGh4567vW_LONG-opaque"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/rest/file/add", "/rest/file/set", "/rest/file/remove"}) {
		t.Fatal(paths)
	}
	if !reflect.DeepEqual(bodies, []map[string]string{{"name": "sata1/中文 空格", "type": "directory"}, {"numbers": "**AbCd0123zZ_LONG-opaque", "name": "sata1/重命名 空格"}, {"numbers": "**EfGh4567vW_LONG-opaque"}}) {
		t.Fatal(bodies)
	}
	before := len(paths)
	for _, p := range []string{"/", "/sata1", "/sata1/../escape", "/sata1/a\\b", "/sata1/a\x00"} {
		if c.CreateDirectory(ctx, p) == nil {
			t.Fatal(p)
		}
	}
	if c.RemoveDirectory(ctx, "*AB/../../file") == nil {
		t.Fatal("unsafe opaque ID accepted")
	}
	for _, id := range []string{"**", "**opaque,other", "**opaque/../../file", "**opaque\n", "**opaque *FF", "*" + strings.Repeat("A", 256)} {
		if c.RemoveDirectory(ctx, id) == nil {
			t.Fatal("invalid opaque ID accepted", id)
		}
	}
	if len(paths) != before {
		t.Fatal("invalid command reached RouterOS")
	}
}
func TestDirectoryWritesNeverRetryUnknownResults(t *testing.T) {
	for _, body := range []string{`{"error":503}`, `malformed`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if body[0] == '{' {
					w.WriteHeader(503)
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			err := NewMutationClient(server.URL, "test", "private").CreateDirectory(context.Background(), "/sata1/folder")
			var unknown *MutationOutcomeUnknownError
			if !errors.As(err, &unknown) || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}
