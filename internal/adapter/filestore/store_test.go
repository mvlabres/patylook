package filestore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"patylook/internal/core/domain"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "images", "products") // does not exist yet: New must create it
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestSaveOpenRemove(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()

	if err := s.Save(ctx, "CAM-1", "principal.jpg", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, "CAM-1", "principal.jpg", []byte("two")); err != nil { // atomic replace
		t.Fatal(err)
	}
	f, mod, err := s.Open(ctx, "CAM-1", "principal.jpg")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if string(data) != "two" || mod.IsZero() {
		t.Errorf("data=%q mod=%v", data, mod)
	}

	// No temporary files are left behind.
	entries, _ := os.ReadDir(filepath.Join(root, "CAM-1"))
	if len(entries) != 1 {
		t.Errorf("folder has %d entries, want 1", len(entries))
	}

	if err := s.Remove(ctx, "CAM-1", "principal.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "CAM-1")); !errors.Is(err, os.ErrNotExist) {
		t.Error("empty product folder should be removed")
	}
	if err := s.Remove(ctx, "CAM-1", "principal.jpg"); err != nil {
		t.Errorf("removing a missing file must not fail: %v", err)
	}
	if _, _, err := s.Open(ctx, "CAM-1", "principal.jpg"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("open missing: %v", err)
	}
}

func TestRemoveKeepsFolderWithOtherFiles(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	_ = s.Save(ctx, "P", "principal.jpg", []byte("a"))
	_ = s.Save(ctx, "P", "imagem-01.jpg", []byte("b"))
	_ = s.Remove(ctx, "P", "imagem-01.jpg")
	if _, err := os.Stat(filepath.Join(root, "P", "principal.jpg")); err != nil {
		t.Errorf("remaining file lost: %v", err)
	}
}

func TestPathTraversalIsRejected(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := [][2]string{
		{"..", "secret.txt"},
		{"CAM", "../secret.txt"},
		{"../products", "x.jpg"},
		{"CAM/../..", "secret.txt"},
		{"CAM", `..\secret.txt`},
		{"", "principal.jpg"},
		{"CAM", ""},
		{".hidden", "x.jpg"},
		{"CAM", ".upload-123"},
		{"C:", "x.jpg"},
		{"CAM", "a/b.jpg"},
	}
	for _, c := range bad {
		if err := s.Save(ctx, c[0], c[1], []byte("x")); err == nil {
			t.Errorf("Save(%q, %q) must fail", c[0], c[1])
		}
		if f, _, err := s.Open(ctx, c[0], c[1]); err == nil {
			f.Close()
			t.Errorf("Open(%q, %q) must fail", c[0], c[1])
		}
		if err := s.Remove(ctx, c[0], c[1]); err == nil {
			t.Errorf("Remove(%q, %q) must fail", c[0], c[1])
		}
	}
	if data, _ := os.ReadFile(outside); string(data) != "secret" {
		t.Error("file outside the root was modified")
	}
}

func TestOpenDirectoryIsNotFound(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	_ = s.Save(ctx, "CAM", "principal.jpg", []byte("x"))
	if _, _, err := s.Open(ctx, "CAM", "CAM"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}
