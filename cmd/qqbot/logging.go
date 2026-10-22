package main

// Optional file logging next to stdout (which systemd forwards to journald).
// Files are rotated by size so a long-running bot cannot fill the disk:
// dicegirl.log → .1 → .2 → .3, oldest dropped.

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logMaxBytes = 10 << 20 // 10 MiB per file
	logKeep     = 3        // rotated files kept
)

type rotatingWriter struct {
	path string
	max  int64
	keep int

	mu   sync.Mutex
	f    *os.File
	size int64
}

func newRotatingWriter(path string, max int64, keep int) (*rotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &rotatingWriter{path: path, max: max, keep: keep}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

// openLocked opens the current file for appending and records its size.
// Callers hold mu (or run before the writer is shared).
func (w *rotatingWriter) openLocked() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if w.size+int64(len(p)) > w.max {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.keep))
	for i := w.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.openLocked()
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// newLogger builds the process logger. With DICEGIRL_BOT_LOG_FILE set, lines
// go to both stdout (journald) and the rotated file; otherwise stdout only.
func newLogger(cfg *botConfig) (*log.Logger, io.Closer, error) {
	out := io.Writer(os.Stdout)
	var closer io.Closer
	if cfg.LogFile != "" {
		rw, err := newRotatingWriter(cfg.LogFile, logMaxBytes, logKeep)
		if err != nil {
			return nil, nil, err
		}
		out = io.MultiWriter(os.Stdout, rw)
		closer = rw
	}
	return log.New(out, "qqbot ", log.LstdFlags|log.Lmsgprefix), closer, nil
}
