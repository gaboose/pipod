package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gaboose/pipod/internal/iio"
	"github.com/mholt/archives"
)

func downloader(url string) (io.ReadCloser, int64) {
	ctx, c := iio.ContextCloser()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return c.WithErrReader(iio.NewErrorReader(fmt.Errorf("failed to make request"))), 0
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return c.WithErrReader(iio.NewErrorReader(fmt.Errorf("failed to make request"))), 0
	}

	return c.WithReadCloser(resp.Body), resp.ContentLength
}

func verifier(rc io.ReadCloser, expectedSum string) io.ReadCloser {
	pr, pw := io.Pipe()
	h := sha256.New()
	mw := io.MultiWriter(pw, h)

	go func() {
		_, err := io.Copy(mw, rc)
		if err != nil {
			pw.CloseWithError(err)
			return
		}

		got := h.Sum(nil)
		gotHex := hex.EncodeToString(got)

		if gotHex != expectedSum {
			pw.CloseWithError(fmt.Errorf("checksum mismatch: got %s, want %s", gotHex, expectedSum))
			return
		}

		pw.Close()
	}()

	return iio.Closer(rc.Close).WithReadCloser(pr)
}

func decompresser(rc io.ReadCloser, url string) io.ReadCloser {
	ctx, c := iio.ContextCloser()
	c = iio.Closer(rc.Close).ChainCloser(c)

	format, identifiedReader, err := archives.Identify(ctx, url, rc)
	if errors.Is(err, archives.NoMatch) {
		return c.WithReader(identifiedReader)
	} else if err != nil {
		return c.WithErrReader(fmt.Errorf("failed to identify compression: %w", err))
	}

	decomp, ok := format.(archives.Decompressor)
	if !ok {
		return c.WithErrReader(fmt.Errorf("no decompressor for format: %T", format))
	}

	decompReader, err := decomp.OpenReader(identifiedReader)
	if err != nil {
		return c.WithErrReader(fmt.Errorf("failed to open decompressor reader: %w", err))
	}

	return c.WithReadCloser(decompReader)
}

func save(rc io.ReadCloser, dest string) error {
	defer rc.Close()

	outFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, rc)
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", dest, err)
	}

	if err := os.Rename(dest, dest); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", dest, dest, err)
	}

	return nil
}

func progress(rc io.ReadCloser, total int64, w io.Writer) io.ReadCloser {
	var bytesRead int64
	tck := time.NewTicker(40 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	printDone := make(chan struct{})

	go func() {
		var lastDisplay string

		for {
			select {
			case <-tck.C:
				current := atomic.LoadInt64(&bytesRead)

				display := progressString(current, total)
				if display == lastDisplay {
					continue
				}

				fmt.Fprintf(w, "\r%s", display)
				lastDisplay = display
			case <-ctx.Done():
				current := atomic.LoadInt64(&bytesRead)
				fmt.Fprintf(w, "\r%s\n", progressString(current, total))
				close(printDone)
				return
			}
		}
	}()

	c := iio.Closer(rc.Close)
	c = c.ChainCloser(iio.Closer(func() error {
		// using a cancellable context instead of a "closing" channel so that
		// Close can safely be called multiple times
		cancel()
		<-printDone
		return nil
	}))

	return c.WithReader(iio.Reader(func(p []byte) (n int, err error) {
		n, err = rc.Read(p)
		atomic.AddInt64(&bytesRead, int64(n))
		return
	}))
}

func byteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}

func progressString(current, total int64) string {
	const WIDTH = 40

	if total == 0 {
		fillPos := int(time.Now().Unix() % WIDTH)
		bar := strings.Repeat(" ", fillPos) + "=" + strings.Repeat(" ", WIDTH-fillPos-1)
		return fmt.Sprintf("[%s] %-10s", bar, byteCountIEC(current))
	}

	filled := int(float64(current) / float64(total) * float64(WIDTH))
	if filled > WIDTH {
		filled = WIDTH
	}

	bar := strings.Repeat("=", filled)
	if filled < WIDTH {
		bar += ">" + strings.Repeat(" ", WIDTH-filled-1)
	}
	ratio := fmt.Sprintf("%s / %s", byteCountIEC(current), byteCountIEC(total))

	return fmt.Sprintf("[%s] %-23s", bar, ratio)
}
