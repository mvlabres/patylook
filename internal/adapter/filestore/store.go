// Package filestore implements port.ImageStorage on the local file system.
package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// segment accepts the names this application generates: no separators, no leading dot.
var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Store keeps files under root/<folder>/<name>.
type Store struct{ root string }

var _ port.ImageStorage = (*Store)(nil)

// New ensures the root directory exists.
func New(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("criar pasta de imagens %s: %w", abs, err)
	}
	return &Store{root: abs}, nil
}

// Root is the absolute directory holding the product folders.
func (s *Store) Root() string { return s.root }

// resolve validates both segments, so a crafted folder or name can never leave root.
func (s *Store) resolve(folder, name string) (dir, file string, err error) {
	for _, seg := range []string{folder, name} {
		if !segment.MatchString(seg) || seg == ".." {
			return "", "", fmt.Errorf("%w: %q", domain.ErrNotFound, seg)
		}
	}
	dir = filepath.Join(s.root, folder)
	return dir, filepath.Join(dir, name), nil
}

func (s *Store) Save(_ context.Context, folder, name string, data []byte) error {
	dir, file, err := s.resolve(folder, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Write to a temporary file and rename, so readers never see a partial image.
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, file); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (s *Store) Remove(_ context.Context, folder, name string) error {
	dir, file, err := s.resolve(folder, name)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_ = os.Remove(dir) // succeeds only when the folder is empty
	return nil
}

func (s *Store) Open(_ context.Context, folder, name string) (io.ReadSeekCloser, time.Time, error) {
	_, file, err := s.resolve(folder, name)
	if err != nil {
		return nil, time.Time{}, err
	}
	f, err := os.Open(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, time.Time{}, domain.ErrNotFound
		}
		return nil, time.Time{}, err
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		return nil, time.Time{}, domain.ErrNotFound
	}
	return f, info.ModTime(), nil
}
