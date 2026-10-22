package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"dicegirl/internal/apperr"
)

// LoadBuiltins compiles every *.yaml rulebook embedded into the binary.
// Built-in and user-provided books use the same schema and code path.
func LoadBuiltins(fsys fs.FS, roller Roller) ([]*Book, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, apperr.Wrap("engine.LoadBuiltins", apperr.ConfigInvalid, "cannot list built-in rulebooks", err)
	}
	sort.Strings(names)
	books := make([]*Book, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, apperr.Wrap("engine.LoadBuiltins", apperr.ConfigInvalid,
				"cannot read built-in rulebook "+name, err)
		}
		book, err := ParseBook(data, "builtin:"+name, roller)
		if err != nil {
			return nil, err
		}
		books = append(books, book)
	}
	return books, nil
}

// LoadDir compiles user rulebooks from <data-root>/rulebooks/. A missing
// directory yields no books; any other read or parse failure is an error.
func LoadDir(dir string, roller Roller) ([]*Book, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, apperr.Wrap("engine.LoadDir", apperr.StorageReadFailed,
			"cannot read rulebook directory "+dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".yaml" || filepath.Ext(e.Name()) == ".yml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	books := make([]*Book, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, apperr.Wrap("engine.LoadDir", apperr.StorageReadFailed,
				"cannot read rulebook "+path, err)
		}
		book, err := ParseBook(data, path, roller)
		if err != nil {
			return nil, err
		}
		books = append(books, book)
	}
	return books, nil
}
