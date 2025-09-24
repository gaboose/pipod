package imagesync

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/afero-guestfs/libguestfs.org/guestfs"
	"github.com/spf13/afero"
)

type TarReader struct {
	tarReader *tar.Reader
}

func NewTarReader(tr *tar.Reader) *TarReader {
	return &TarReader{
		tarReader: tr,
	}
}

func (c *TarReader) ToGuestFS(g *guestfs.Guestfs, opts ...Option) error {
	var oo options
	for _, o := range opts {
		o(&oo)
	}

	gfs := aferoguestfs.New(g)
	diskPathMap, err := walkDiskTSK(g)
	if err != nil {
		return fmt.Errorf("failed to walk disk: %w", err)
	}

	var preserveBaseDirPath string
	var preserveBaseDirModTime time.Time
	preserveBaseDir := func(baseDirPath string) error {
		if baseDirPath == preserveBaseDirPath {
			return nil
		}

		if preserveBaseDirPath != "" {
			if err := gfs.Chtimes(preserveBaseDirPath, preserveBaseDirModTime, preserveBaseDirModTime); err != nil {
				return fmt.Errorf("failed to preserve base dir modtime: %s: %w", preserveBaseDirPath, err)
			}
			preserveBaseDirPath = ""
		}

		if baseDirPath != "" {
			fi, err := gfs.Stat(baseDirPath)
			if err != nil {
				return fmt.Errorf("failed to stat base dir: %s: %w", baseDirPath, err)
			}

			preserveBaseDirPath = baseDirPath
			preserveBaseDirModTime = fi.ModTime()
		}

		return nil
	}

	for {
		hdr, err := c.tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("failed to get next file in tar: %w", err)
		}

		path := filepath.Join("/", hdr.Name)
		upd := PathUpdate{
			Path: path,
		}

		diskFileInfo, err := gfs.Lstat(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to lstat: %s: %w", path, err)
		}

		if err = addFileIfNew(gfs, hdr, c.tarReader, diskFileInfo, &upd.Update, preserveBaseDir); err != nil {
			return fmt.Errorf("failed to add file: %s: %w", path, err)
		}

		if upd.Added {
			diskFileInfo, err = gfs.Lstat(path)
			if err != nil {
				return fmt.Errorf("failed to lstat: %s: %w", path, err)
			}
		}

		if err = updateStatIfDiff(g, hdr, diskFileInfo, &upd.Update); err != nil {
			return fmt.Errorf("failed to update stat: %s: %w", path, err)
		}

		if !upd.IsEmpty() {
			for _, updatedFn := range oo.updated {
				updatedFn(upd)
			}
		}

		delete(diskPathMap, path)
	}

	if err := deletePaths(gfs, diskPathMap, oo, preserveBaseDir); err != nil {
		return fmt.Errorf("failed to delete paths: %w", err)
	}

	if err = preserveBaseDir(""); err != nil {
		return err
	}

	return nil
}

func walkDiskTSK(g *guestfs.Guestfs) (map[string]struct{}, error) {
	mps, err := g.Mountpoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get mount points: %w", err)
	}

	var partDevice string
	for d, mp := range mps {
		if mp == "/" {
			partDevice = d
			break
		}
	}

	if partDevice == "" {
		return nil, fmt.Errorf("nothing mounted at root")
	}

	entries, err := g.Filesystem_walk(partDevice)
	if err != nil {
		return nil, fmt.Errorf("failed to walk disk: %w", err)
	}

	diskPathMap := make(map[string]struct{}, len(*entries))
	for _, e := range *entries {
		diskPathMap[filepath.Join("/", e.Tsk_name)] = struct{}{}
	}

	return diskPathMap, nil
}

func addFileIfNew(gfs *aferoguestfs.Fs, hdr *tar.Header, tr *tar.Reader, diskFileInfo fs.FileInfo, upd *Update, preserveBaseDir func(string) error) error {
	path := filepath.Join("/", hdr.Name)
	fileInDisk := diskFileInfo != nil
	tarFileInfo := hdr.FileInfo()
	isSymlink := tarFileInfo.Mode()&os.ModeSymlink != 0
	isLink := hdr.Typeflag == tar.TypeLink

	switch {
	case isLink:
		abslinkname := filepath.Join("/", hdr.Linkname)
		if fileInDisk {
			targetFileInfo, err := gfs.Stat(abslinkname)
			if err != nil {
				return fmt.Errorf("failed to stat link target: %s: %w", path, err)
			}
			if diskFileInfo.Sys().(*guestfs.StatNS).St_ino != targetFileInfo.Sys().(*guestfs.StatNS).St_ino {
				if err := preserveBaseDir(filepath.Dir(path)); err != nil {
					return err
				}
				if err = gfs.Remove(path); err != nil {
					return fmt.Errorf("failed to remove link: %s: %w", path, err)
				}
				fileInDisk = false
			}
		}

		if !fileInDisk {
			if err := preserveBaseDir(filepath.Dir(path)); err != nil {
				return err
			}
			err := gfs.Link(abslinkname, path)
			if err != nil {
				return fmt.Errorf("failed to make link: %s: %w", path, err)
			}
			upd.Added = true
		}

	case tarFileInfo.Mode().IsRegular():
		if !fileInDisk || !filesEqual(tarFileInfo, diskFileInfo) {
			if err := preserveBaseDir(filepath.Dir(path)); err != nil {
				return err
			}
			err := afero.WriteReader(gfs, path, tr)
			if err != nil {
				return fmt.Errorf("failed to write file: %s: %w", path, err)
			}
			upd.Added = true
		}
	case tarFileInfo.IsDir():
		if !fileInDisk {
			if err := preserveBaseDir(filepath.Dir(path)); err != nil {
				return err
			}
			err := gfs.Mkdir(path, tarFileInfo.Mode().Perm())
			if err != nil {
				return fmt.Errorf("failed to make file: %s: %w", path, err)
			}
			upd.Added = true
		}
	case isSymlink:
		if fileInDisk {
			target, err := gfs.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read link: %s: %w", path, err)
			}

			if target != hdr.Linkname {
				if err := preserveBaseDir(filepath.Dir(path)); err != nil {
					return err
				}
				err = gfs.Remove(path)
				if err != nil {
					return fmt.Errorf("failed to remove link: %s: %w", path, err)
				}
				fileInDisk = false
			}
		}

		if !fileInDisk {
			if err := preserveBaseDir(filepath.Dir(path)); err != nil {
				return err
			}
			err := gfs.SymlinkIfPossible(hdr.Linkname, path)
			if err != nil {
				return fmt.Errorf("failed to make link: %s: %w", path, err)
			}
			upd.Added = true
		}
	default:
		return fmt.Errorf("unexpected file type: %s: %d", path, tarFileInfo.Mode()&os.ModeType)
	}

	return nil
}

func updateStatIfDiff(g *guestfs.Guestfs, hdr *tar.Header, diskFileInfo fs.FileInfo, upd *Update) error {
	path := filepath.Join("/", hdr.Name)
	gfs := aferoguestfs.New(g)
	tarFileInfo := hdr.FileInfo()
	isSymlink := tarFileInfo.Mode()&os.ModeSymlink != 0
	stat := diskFileInfo.Sys().(*guestfs.StatNS)

	if int64(hdr.Uid) != stat.St_uid || int64(hdr.Gid) != stat.St_gid {
		var err error
		if isSymlink {
			err = g.Lchown(hdr.Uid, hdr.Gid, path)
		} else {
			err = gfs.Chown(path, hdr.Uid, hdr.Gid)
		}
		if err != nil {
			return fmt.Errorf("failed to chown: %s: %w", path, err)
		}

		upd.Uid = ptr(hdr.Uid)
		upd.Gid = ptr(hdr.Gid)
	}

	if !isSymlink && tarFileInfo.Mode() != diskFileInfo.Mode() {
		err := gfs.Chmod(path, tarFileInfo.Mode().Perm())
		if err != nil {
			return fmt.Errorf("failed to chmod: %s: %w", path, err)
		}
		diskFileInfo, _ = gfs.Lstat(path)

		upd.Perm = ptr(tarFileInfo.Mode())
	}

	if !tarFileInfo.ModTime().Equal(diskFileInfo.ModTime()) {
		err := gfs.Chtimes(path, hdr.ModTime, hdr.ModTime)
		if err != nil {
			return fmt.Errorf("failed to chtimes: %s: %w", path, err)
		}

		upd.ModTime = ptr(hdr.ModTime)
	}

	return nil
}

func deletePaths(gfs *aferoguestfs.Fs, diskPathMap map[string]struct{}, oo options, preserveBaseDir func(string) error) error {
	toDelete := make([]string, 0, len(diskPathMap))
	for entry := range diskPathMap {
		toDelete = append(toDelete, entry)
	}
	sort.Strings(toDelete)

	for _, entry := range toDelete {
		// ignore root path
		if entry == "/" {
			continue
		}

		// ignore paths that don't exist
		// some paths appear in the Filesystem_walk results but can't be
		// accessed via Lstat or removed, these for example include:
		// - /$OrphanFiles
		// - /lib/modules/5.15.78-v8/build/include/config/\xa
		// - /usr/share/mime/application/\x2
		// - /var/lib/opkg/info/\xf
		// - /var/lib/opkg/info/'
		// - /var/lib/opkg/info/^
		// - /var/lib/opkg/info/\x81
		if _, err := gfs.Lstat(entry); errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err := preserveBaseDir(filepath.Dir(entry)); err != nil {
			return err
		}

		err := gfs.RemoveAll(entry)
		if err != nil {
			return fmt.Errorf("failed to remove: %s: %w", entry, err)
		}

		for _, updatedFn := range oo.updated {
			updatedFn(PathUpdate{
				Path: entry,
				Update: Update{
					Deleted: true,
				},
			})
		}
	}

	return nil
}

func filesEqual(left os.FileInfo, right os.FileInfo) bool {
	return left.Size() == right.Size() && left.ModTime().Equal(right.ModTime())
}

func ptr[T any](t T) *T {
	return &t
}
