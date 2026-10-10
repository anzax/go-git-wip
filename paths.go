package wip

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func (r *Repository) normalizePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]bool)
	for _, path := range paths {
		if path == "" || strings.ContainsRune(path, 0) {
			return nil, &Error{Kind: KindInvalidOption, Op: "paths", Message: "paths must be nonempty and contain no NUL"}
		}
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(r.dir, full)
		}
		rel, err := filepath.Rel(r.root, filepath.Clean(full))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, &Error{Kind: KindInvalidOption, Op: "paths", Message: "path is outside the repository", Err: err}
		}
		if err := r.ensureNoSymlinkDirectories(rel); err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			result = append(result, rel)
		}
	}
	return result, nil
}

func (r *Repository) ensureNoSymlinkDirectories(path string) error {
	parts := strings.Split(filepath.FromSlash(path), string(filepath.Separator))
	parent := r.root
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
			continue // Deleted historical paths remain selectable.
		}
		if err != nil {
			return &Error{Kind: KindInvalidOption, Op: "paths", Message: "inspect path directory", Err: err}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &Error{Kind: KindInvalidOption, Op: "paths", Message: "paths through symlink directories are unsupported"}
		}
	}
	return nil
}

func pathSelected(path string, selections []string) bool {
	if len(selections) == 0 {
		return true
	}
	for _, selection := range selections {
		if selection == "." || path == selection || strings.HasPrefix(path, selection+"/") {
			return true
		}
	}
	return false
}
