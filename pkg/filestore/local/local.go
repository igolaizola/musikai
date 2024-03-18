package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type store struct {
	root string
}

func New(root string, debug bool) *store {
	return &store{root: root}
}

func (s *store) Upload(ctx context.Context, path, name string) error {
	dst := filepath.Join(s.root, name)
	if err := copyFile(path, dst); err != nil {
		return fmt.Errorf("local: couldn't copy file %q to %q: %w", path, dst, err)
	}
	return nil
}

func (s *store) Download(ctx context.Context, path, name string) error {
	src := filepath.Join(s.root, name)
	if err := copyFile(src, path); err != nil {
		return fmt.Errorf("local: couldn't copy file %q to %q: %w", src, path, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get the file information of the source file to obtain its permissions
	srcFileInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// Open the destination file for writing. If it does not exist, create it with
	// the same permissions as the source file. If it exists, truncate it.
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcFileInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy the content from the source file to the destination file
	_, err = io.Copy(dstFile, srcFile)
	return err
}
