package corelx

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ProjectMainFile is the conventional entry source inside a project.
const ProjectMainFile = "main.corelx"

// openProject resolves a compile input to a main source path on disk.
//
//   - A `.ncdx` container (a ZIP) is extracted to a temp directory and its
//     main.corelx path is returned, with a cleanup that removes the temp dir.
//   - A plain `.corelx` file or a project directory is used in place (cleanup
//     is a no-op). For a directory, main.corelx inside it is the entry.
//
// This keeps the rest of the compiler filesystem-based: asset resolution and
// the orphan check operate on the (possibly temporary) project directory.
func openProject(path string) (mainPath string, cleanup func(), err error) {
	noop := func() {}
	lower := strings.ToLower(path)

	if strings.HasSuffix(lower, ".ncdx") {
		tmp, err := os.MkdirTemp("", "ncdx-")
		if err != nil {
			return "", noop, err
		}
		if err := unzipInto(path, tmp); err != nil {
			os.RemoveAll(tmp)
			return "", noop, fmt.Errorf("open .ncdx project: %w", err)
		}
		main := filepath.Join(tmp, ProjectMainFile)
		if _, statErr := os.Stat(main); statErr != nil {
			os.RemoveAll(tmp)
			return "", noop, fmt.Errorf("project %s is missing %s", filepath.Base(path), ProjectMainFile)
		}
		return main, func() { os.RemoveAll(tmp) }, nil
	}

	info, statErr := os.Stat(path)
	if statErr == nil && info.IsDir() {
		return filepath.Join(path, ProjectMainFile), noop, nil
	}
	return path, noop, nil
}

// unzipInto extracts a ZIP archive into dst (flat or nested), guarding against
// path traversal.
func unzipInto(archivePath, dst string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dst, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) && target != filepath.Clean(dst) {
			return fmt.Errorf("unsafe path in archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}
