package ui

import (
	"testing"
	"testing/fstest"
)

func TestValidateRequiresReferencedEntry(t *testing.T) {
	files := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte(`<script src="./assets/missing.js"></script>`)},
		"assets/other.js":  &fstest.MapFile{Data: []byte("code")},
		"assets/style.css": &fstest.MapFile{Data: []byte("style")},
	}
	if Validate(files) == nil {
		t.Fatal("missing referenced entry accepted")
	}
	files["assets/missing.js"] = &fstest.MapFile{Data: []byte("code")}
	if err := Validate(files); err != nil {
		t.Fatal(err)
	}
}
