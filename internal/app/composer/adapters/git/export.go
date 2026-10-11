package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitio "github.com/go-git/go-git/v5/utils/ioutil"
)

var (
	errPath     = errors.New("invalid path in git tree")
	errFileMode = errors.New("unsupported git file mode (symlinks and submodules are not supported yet)")
)

func exportTree(ctx context.Context, tree *object.Tree, root string) error {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("export tree: %w", err)
		}

		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("walk tree: %w", err)
		}

		if err = exportEntry(ctx, tree, root, name, entry); err != nil {
			return err
		}
	}
}

func exportEntry(ctx context.Context, tree *object.Tree, root, name string, entry object.TreeEntry) error {
	if !fs.ValidPath(name) {
		return errPath
	}

	if entry.Mode == filemode.Dir {
		return nil
	}

	if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
		return fmt.Errorf("%w at %s", errFileMode, name)
	}

	file, err := tree.File(name)
	if err != nil {
		return fmt.Errorf("read tree file: %w", err)
	}

	return exportFile(ctx, file, filepath.Join(root, filepath.FromSlash(name)))
}

func exportFile(ctx context.Context, file *object.File, path string) (err error) {
	if err = os.MkdirAll(filepath.Dir(path), privateDirectoryMode); err != nil {
		return fmt.Errorf("create file directory: %w", err)
	}

	var mode fs.FileMode = 0o600
	if file.Mode == filemode.Executable {
		mode = 0o700
	}

	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}

	defer func() { err = errors.Join(err, dst.Close()) }()

	src, err := file.Reader()
	if err != nil {
		return fmt.Errorf("open git blob: %w", err)
	}

	defer func() { err = errors.Join(err, src.Close()) }()

	_, err = io.Copy(dst, gitio.NewContextReader(ctx, src))
	if err != nil {
		return fmt.Errorf("copy git blob: %w", err)
	}

	return nil
}
