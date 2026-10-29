package runtime

import (
	"io"
	"os"

	"github.com/aceqbit/seedlog/wal"
)

// FileStorage is a wal.Storage backed by one file; Sync is fsync.
type FileStorage struct {
	f    *os.File
	size int64
}

var _ wal.Storage = (*FileStorage)(nil)

// OpenFile opens or creates the log file at path.
func OpenFile(path string) (*FileStorage, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &FileStorage{f: f, size: st.Size()}, nil
}

// Append implements wal.Storage.
func (s *FileStorage) Append(p []byte) error {
	n, err := s.f.WriteAt(p, s.size)
	s.size += int64(n)
	return err
}

// Sync implements wal.Storage.
func (s *FileStorage) Sync() error { return s.f.Sync() }

// ReadAll implements wal.Storage.
func (s *FileStorage) ReadAll() ([]byte, error) {
	b := make([]byte, s.size)
	if _, err := s.f.ReadAt(b, 0); err != nil && err != io.EOF {
		return nil, err
	}
	return b, nil
}

// Truncate implements wal.Storage.
func (s *FileStorage) Truncate(n int64) error {
	if n < 0 || n > s.size {
		return wal.ErrTruncate
	}
	if err := s.f.Truncate(n); err != nil {
		return err
	}
	s.size = n
	return nil
}

// Close closes the file.
func (s *FileStorage) Close() error { return s.f.Close() }
