package imagesync_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/afero-guestfs/libguestfs.org/guestfs"
	"github.com/gaboose/pipod/imagesync"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

var gfs *guestfs.Guestfs

func TestMain(m *testing.M) {
	var gfsClose func() error
	var err error
	gfs, gfsClose, err = newGuestFS()
	if err != nil {
		panic(err)
	}
	defer gfsClose()

	code := m.Run()

	os.Exit(code)
}

func TestRegularFile(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./test.txt",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}})
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/test.txt",
			Update: imagesync.Update{
				Added:   true,
				Perm:    ptr(fs.ModePerm),
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Delete", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar(nil)
		assert.Nil(t, err)

		// build disk
		err = afero.WriteFile(aferoguestfs.New(gfs), "/test.txt", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/test.txt",
			Update: imagesync.Update{
				Deleted: true,
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Chmod", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./test.txt",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = afero.WriteFile(agfs, "/test.txt", []byte("some text"), 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/test.txt", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/test.txt",
			Update: imagesync.Update{
				Perm: ptr(fs.ModePerm),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Chown", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./test.txt",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Uid:     1000,
				Gid:     1000,
			},
			Body: "some text",
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = afero.WriteFile(agfs, "/test.txt", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/test.txt", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/test.txt",
			Update: imagesync.Update{
				Uid: ptr(1000),
				Gid: ptr(1000),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Overwrite", func(t *testing.T) {
		t.Run("ModTime", func(t *testing.T) {
			err := clear(gfs)
			assert.Nil(t, err)

			// build tar
			bts, err := newTar([]struct {
				Header tar.Header
				Body   string
			}{{
				Header: tar.Header{
					Name:    "./test.txt",
					Mode:    int64(fs.ModePerm),
					ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
				Body: "some text2",
			}})
			assert.Nil(t, err)

			// build disk
			agfs := aferoguestfs.New(gfs)
			err = afero.WriteFile(agfs, "/test.txt", []byte("some text1"), fs.ModePerm)
			assert.Nil(t, err)
			err = agfs.Chtimes("/test.txt", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
			assert.Nil(t, err)

			// sync
			var updates []imagesync.PathUpdate
			tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
			err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
				updates = append(updates, upd)
			}))
			assert.Nil(t, err)

			// assert
			assert.Equal(t, []imagesync.PathUpdate{{
				Path: "/test.txt",
				Update: imagesync.Update{
					Added:   true,
					ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
				},
			}}, updates)

			assertEqualTars(t, bts, gfs)
		})

		t.Run("Size", func(t *testing.T) {
			err := clear(gfs)
			assert.Nil(t, err)

			// build tar
			bts, err := newTar([]struct {
				Header tar.Header
				Body   string
			}{{
				Header: tar.Header{
					Name:    "./test.txt",
					Mode:    int64(fs.ModePerm),
					ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
				Body: "some text2",
			}})
			assert.Nil(t, err)

			// build disk
			agfs := aferoguestfs.New(gfs)
			err = afero.WriteFile(agfs, "/test.txt", []byte("some text"), fs.ModePerm)
			assert.Nil(t, err)
			err = agfs.Chtimes("/test.txt", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
			assert.Nil(t, err)

			// sync
			var updates []imagesync.PathUpdate
			tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
			err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
				updates = append(updates, upd)
			}))
			assert.Nil(t, err)

			// assert
			assert.Equal(t, []imagesync.PathUpdate{{
				Path: "/test.txt",
				Update: imagesync.Update{
					Added:   true,
					ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
				},
			}}, updates)

			assertEqualTars(t, bts, gfs)
		})
	})

	t.Run("Noop", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./test.txt",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = afero.WriteFile(agfs, "/test.txt", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/test.txt", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate(nil), updates)
		assertEqualTars(t, bts, gfs)
	})
}

func TestDir(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/etc",
			Update: imagesync.Update{
				Added:   true,
				Perm:    ptr(fs.ModePerm | fs.ModeDir),
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Delete", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar(nil)
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/etc",
			Update: imagesync.Update{
				Deleted: true,
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Chmod", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/etc", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/etc",
			Update: imagesync.Update{
				Perm: ptr(fs.ModePerm | fs.ModeDir),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Chown", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    0644,
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Uid:     1000,
				Gid:     1000,
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/etc", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/etc",
			Update: imagesync.Update{
				Uid: ptr(1000),
				Gid: ptr(1000),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("ModTime", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    0644,
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/etc", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/etc",
			Update: imagesync.Update{
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Noop", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    0644,
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/etc", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate(nil), updates)
		assertEqualTars(t, bts, gfs)
	})

	t.Run("PreserveModTime", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./etc/",
				Mode:    0644,
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}, {
			Header: tar.Header{
				Name:    "./etc/var/",
				Mode:    0644,
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}, {
			Header: tar.Header{
				Name:    "./etc/test.txt",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}, {
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./etc/symlink",
				Linkname: "/target",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}, {
			Header: tar.Header{
				Typeflag: tar.TypeLink,
				Name:     "./etc/hardlink",
				Linkname: "./etc/test.txt",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Mkdir("/etc", 0644)
		assert.Nil(t, err)
		err = agfs.Chtimes("/etc", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)
		err = afero.WriteFile(agfs, "/etc/todelete", []byte("some more text"), fs.ModePerm)
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		etcFileInfo, err := agfs.Stat("/etc")
		assert.Nil(t, err)
		assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local(), etcFileInfo.ModTime())

		assertEqualTars(t, bts, gfs)
	})
}

func TestSymlink(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./link",
				Linkname: "/target",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Added:   true,
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Delete", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar(nil)
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Symlink("/target", "/link")
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Deleted: true,
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Chown", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./link",
				Linkname: "/target",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Uid:      1000,
				Gid:      1000,
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Symlink("/target", "/link")
		assert.Nil(t, err)
		err = agfs.Chtimes("/link", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Uid: ptr(1000),
				Gid: ptr(1000),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("ModTime", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./link",
				Linkname: "/target",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Symlink("/target", "/link")
		assert.Nil(t, err)
		err = agfs.Chtimes("/link", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Overwrite", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./link",
				Linkname: "/target2",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Symlink("/target1", "/link")
		assert.Nil(t, err)
		err = agfs.Chtimes("/link", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Added:   true,
				ModTime: ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Local()),
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Noop", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     "./link",
				Linkname: "/target",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = agfs.Symlink("/target", "/link")
		assert.Nil(t, err)
		err = agfs.Chtimes("/link", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate(nil), updates)
		assertEqualTars(t, bts, gfs)
	})
}

func TestLink(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./atarget",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}, {
			Header: tar.Header{
				Typeflag: tar.TypeLink,
				Name:     "./link",
				Linkname: "./atarget",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = afero.WriteFile(agfs, "/atarget", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/atarget", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Added: true,
			},
		}}, updates)

		linkfi, err := agfs.Stat("/link")
		assert.Nil(t, err)
		targetfi, err := agfs.Stat("/atarget")
		assert.Nil(t, err)
		assert.Equal(t, targetfi.Sys().(*guestfs.StatNS).St_ino, linkfi.Sys().(*guestfs.StatNS).St_ino)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Delete", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./atarget",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		afero.WriteFile(agfs, "/atarget", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		agfs.Chtimes("/atarget", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		err = agfs.Link("/atarget", "/link")
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Deleted: true,
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Overwrite", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./atarget1",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}, {
			Header: tar.Header{
				Name:    "./atarget2",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some more text",
		}, {
			Header: tar.Header{
				Typeflag: tar.TypeLink,
				Name:     "./link",
				Linkname: "./atarget2",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		afero.WriteFile(agfs, "/atarget1", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/atarget1", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)
		afero.WriteFile(agfs, "/atarget2", []byte("some more text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/atarget2", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)
		err = agfs.Link("/atarget1", "/link")
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Added: true,
			},
		}}, updates)

		assertEqualTars(t, bts, gfs)
	})

	t.Run("Noop", func(t *testing.T) {
		err := clear(gfs)
		assert.Nil(t, err)

		// build tar
		bts, err := newTar([]struct {
			Header tar.Header
			Body   string
		}{{
			Header: tar.Header{
				Name:    "./atarget",
				Mode:    int64(fs.ModePerm),
				ModTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Body: "some text",
		}, {
			Header: tar.Header{
				Typeflag: tar.TypeLink,
				Name:     "./link",
				Linkname: "./atarget",
				Mode:     int64(fs.ModePerm),
				ModTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}})
		assert.Nil(t, err)

		// build disk
		agfs := aferoguestfs.New(gfs)
		err = afero.WriteFile(agfs, "/atarget", []byte("some text"), fs.ModePerm)
		assert.Nil(t, err)
		err = agfs.Chtimes("/atarget", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		assert.Nil(t, err)

		// sync
		var updates []imagesync.PathUpdate
		tarReader := imagesync.NewTarReader(tar.NewReader(bytes.NewBuffer(bts)))
		err = tarReader.ToGuestFS(gfs, imagesync.WithUpdateHook(func(upd imagesync.PathUpdate) {
			updates = append(updates, upd)
		}))
		assert.Nil(t, err)

		// assert
		assert.Equal(t, []imagesync.PathUpdate{{
			Path: "/link",
			Update: imagesync.Update{
				Added: true,
			},
		}}, updates)

		linkfi, err := agfs.Stat("/link")
		assert.Nil(t, err)
		targetfi, err := agfs.Stat("/atarget")
		assert.Nil(t, err)
		assert.Equal(t, targetfi.Sys().(*guestfs.StatNS).St_ino, linkfi.Sys().(*guestfs.StatNS).St_ino)

		assertEqualTars(t, bts, gfs)
	})
}

func newGuestFS() (g *guestfs.Guestfs, closeFn func() error, err error) {
	const size int64 = 4 * 1024 * 1024

	f, err := os.CreateTemp("", "guestfs-*.img")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp file failed: %w", err)
	}
	tmpPath := f.Name()
	closeFn = func() error {
		return os.Remove(tmpPath)
	}

	if cerr := f.Close(); cerr != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("close temp file failed: %w", cerr)
	}

	if err := os.Truncate(tmpPath, size); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("truncate temp file failed: %w", err)
	}

	g, err = guestfs.Create()
	if err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("guestfs create failed: %w", err)
	}
	closeFn = func() error {
		if err := g.Close(); err != nil {
			return err
		}
		return os.Remove(tmpPath)
	}

	if err := g.Add_drive(tmpPath, nil); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("add drive failed: %w", err)
	}

	if err := g.Launch(); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("launch failed: %w", err)
	}

	devices, err := g.List_devices()
	if err != nil || len(devices) == 0 {
		_ = closeFn()
		return nil, nil, fmt.Errorf("no devices found: %w", err)
	}
	device := devices[0]

	if err := g.Mkfs("ext4", device, nil); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("mkfs failed: %w", err)
	}

	if err := g.Mount(device, "/"); err != nil {
		_ = closeFn()
		return nil, nil, fmt.Errorf("mount failed: %w", err)
	}
	closeFn = func() error {
		if err := g.Umount_all(); err != nil {
			return err
		}
		if err := g.Close(); err != nil {
			return err
		}
		return os.Remove(tmpPath)
	}

	return g, closeFn, nil
}

func newTar(files []struct {
	Header tar.Header
	Body   string
}) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	tarWriter := tar.NewWriter(buf)

	for _, tf := range files {
		tf.Header.Size = int64(len(tf.Body))
		if err := tarWriter.WriteHeader(&tf.Header); err != nil {
			return nil, fmt.Errorf("failed to write header: %s: %w", tf.Header.Name, err)
		}

		if len(tf.Body) > 0 {
			if _, err := io.Copy(tarWriter, bytes.NewBufferString(tf.Body)); err != nil {
				return nil, fmt.Errorf("failed to write file: %s: %w", tf.Header.Name, err)
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	return buf.Bytes(), nil
}

func readFullTar(bts []byte) ([]struct {
	Header tar.Header
	Body   string
}, error) {
	ret := []struct {
		Header tar.Header
		Body   string
	}{}

	tarReader := tar.NewReader(bytes.NewBuffer(bts))
	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to read next: %w", err)
		}

		body := bytes.NewBuffer(nil)
		_, err = io.Copy(body, tarReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %s: %w", hdr.Name, err)
		}

		ret = append(ret, struct {
			Header tar.Header
			Body   string
		}{
			Header: *hdr,
			Body:   body.String(),
		})
	}

	return ret, nil
}

func assertEqualTars(t *testing.T, expectedTarBytes []byte, gfs *guestfs.Guestfs) {
	tmpTar, err := os.CreateTemp("", "guestfs-*.tar")
	assert.Nil(t, err)
	err = tmpTar.Close()
	assert.Nil(t, err)
	defer os.Remove(tmpTar.Name())

	err = gfs.Tar_out("/", tmpTar.Name(), nil)
	assert.Nil(t, err)

	actualTarBytes, err := os.ReadFile(tmpTar.Name())
	assert.Nil(t, err)

	expectedFiles, err := readFullTar(expectedTarBytes)
	assert.Nil(t, err)

	actualFiles, err := readFullTar(actualTarBytes)
	assert.Nil(t, err)

	normalize := func(files *[]struct {
		Header tar.Header
		Body   string
	}) {
		for i := 0; i < len(*files); i++ {
			// ignore the root folder
			if (*files)[i].Header.Name == "./" {
				(*files) = append((*files)[:i], (*files)[i+1:]...)
				i--
				continue
			}

			// ignore user and group names
			(*files)[i].Header.Uname = ""
			(*files)[i].Header.Gname = ""

			// ignore format
			(*files)[i].Header.Format = 0

			// normalize hard links (link to the alphabetically first path)
			if (*files)[i].Header.Typeflag == tar.TypeLink {
				if (*files)[i].Header.Name < (*files)[i].Header.Linkname {
					for j, target := range (*files)[:i] {
						if (*files)[i].Header.Linkname == target.Header.Name {
							(*files)[j].Header.Name = (*files)[i].Header.Name
							(*files)[i].Header.Name = (*files)[i].Header.Linkname
							(*files)[i].Header.Linkname = (*files)[j].Header.Name
						}
					}
				}
			}
		}

		sort.Slice(*files, func(i, j int) bool {
			return (*files)[i].Header.Name < (*files)[j].Header.Name
		})
	}

	normalize(&expectedFiles)
	normalize(&actualFiles)
	assert.Equal(t, expectedFiles, actualFiles)
}

func clear(gfs *guestfs.Guestfs) error {
	agfs := aferoguestfs.New(gfs)
	root, err := agfs.Open("/")
	if err != nil {
		return fmt.Errorf("failed to open root: %w", err)
	}

	dirnames, err := root.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("failed to read dir names: %w", err)
	}

	for _, dirname := range dirnames {
		err = agfs.RemoveAll(filepath.Join("/", dirname))
		if err != nil {
			return fmt.Errorf("failed to remove %s: %w", dirname, err)
		}
	}

	return nil
}

func ptr[T any](t T) *T {
	return &t
}
