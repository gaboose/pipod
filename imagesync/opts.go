package imagesync

import (
	"io/fs"
	"time"
)

type options struct {
	updated []func(upd PathUpdate)
}

type Option func(o *options)

type PathUpdate struct {
	Path string
	Update
}

type Update struct {
	Added   bool
	Deleted bool
	Perm    *fs.FileMode
	Uid     *int
	Gid     *int
	ModTime *time.Time
}

func (upd Update) IsEmpty() bool {
	return upd == Update{}
}

func WithUpdateHook(fn func(upd PathUpdate)) Option {
	return func(o *options) {
		o.updated = append(o.updated, fn)
	}
}
