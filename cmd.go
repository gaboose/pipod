package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/aferosync"
	"github.com/gaboose/pipod/internal/guestfish"
	"github.com/gaboose/pipod/internal/iio"
	"github.com/gaboose/pipod/internal/podman"
	"github.com/mholt/archives"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/afero"
)

const (
	TEMP_DIR = "tmp"
)

type ContainerCmd struct {
	Build ContainerBuildCmd `cmd:"" help:"Build a container image from a pipod.toml"`
}

type ContainerBuildCmd struct {
	Spec string `default:"pipod.toml" help:"Path to pipod.toml" type:"existingfile"`
}

func (b *ContainerBuildCmd) Run() error {
	tomlBts, err := os.ReadFile(b.Spec)
	if err != nil {
		return fmt.Errorf("failed to read toml: %w", err)
	}

	var spec Spec
	if err := toml.Unmarshal(tomlBts, &spec); err != nil {
		return fmt.Errorf("failed to load TOML: %w", err)
	}

	if err := spec.validate(); err != nil {
		return fmt.Errorf("toml: %w", err)
	}

	if err := os.RemoveAll(TEMP_DIR); err != nil {
		return fmt.Errorf("failed to remove tmp dir: %w", err)
	}
	if err := os.Mkdir(TEMP_DIR, 0755); err != nil {
		return fmt.Errorf("failed to create tmp dir: %w", err)
	}
	defer os.RemoveAll(TEMP_DIR)

	var images []string
	for platformName, platform := range spec.Platform {
		workingPath := removeArchiveExt(filepath.Join(TEMP_DIR, filepath.Base(platform.Labels.SourceURL)))

		fmt.Printf("Downloading %s...\n", platform.Labels.SourceURL)
		rc, n := downloader(platform.Labels.SourceURL)
		rc = progress(rc, n, os.Stdout)
		rc = verifier(rc, platform.Labels.SourceSHA256)
		rc = decompresser(rc, platform.Labels.SourceURL)
		if err := save(rc, workingPath+".part"); err != nil {
			return fmt.Errorf("%s: %w", platform.Labels.SourceURL, err)
		}

		if err := os.Rename(workingPath+".part", workingPath); err != nil {
			return fmt.Errorf("failed to rename: %w", err)
		}

		image := strings.ReplaceAll(platformName, ":", "-")
		image = strings.ReplaceAll(image, "/", "-")
		image = spec.Name + "-" + image

		fmt.Printf("Importing %s...\n", image)
		readCloser := guestfish.TarOut(workingPath, "/dev/"+platform.Labels.SourcePartitionsImport)

		if err := podman.Import(
			image,
			readCloser,
			podman.WithPlatform(platformName),
			podman.WithLabels(spec.Labels),
			podman.WithLabelsToml(platform.Labels),
		); err != nil {
			return fmt.Errorf("failed to import podman image: %w", err)
		}

		images = append(images, image)
	}

	fmt.Printf("Creating manifest %s...\n", spec.Name)
	if err := podman.CreateManifest(spec.Name, images); err != nil {
		return fmt.Errorf("failed to create manifest: %w", err)
	}

	fmt.Println("Build complete.")
	fmt.Println(spec.Name)
	return nil
}

type DiskCmd struct {
	Build DiskBuildCmd `cmd:"" help:"Build a disk image from a Containerfile"`
	Sync  DiskSyncCmd  `cmd:"" help:"Sync a disk image from a tar or a container image"`
}

type DiskBuildCmd struct {
	Out           string `help:"File to write to" default:"build/out.img"`
	Platform      string `default:"linux/arm64" help:"Set the OS/ARCH[/VARIANT] of the image"`
	ForceDownload bool   `help:"Force download even if the output file exists"`
	Verbose       bool   `short:"v" help:"Print paths of all synced files"`
}

const (
	green          = "\033[32m"
	reset          = "\033[0m"
	cursorUp       = "\033[%dA"
	eraseInDisplay = "\033[J"
)

var aferoSyncStdout = iio.Writer(os.Stdout.Write).WithPrefix(green + "[aferosync]" + reset)

func (b *DiskBuildCmd) Run(kctx *kong.Context) error {
	fmt.Printf("Building . for platform %s...\n", b.Platform)
	image, err := podman.Build(b.Platform)
	if err != nil {
		return fmt.Errorf("failed to build podman image: %w", err)
	}

	var labels podman.PipodLabels
	if err := image.UnmarshalLabelsToml(&labels); err != nil {
		return fmt.Errorf("failed to get image labels: %w", err)
	}

	if err := labels.Validate(); err != nil {
		return fmt.Errorf("labels validation failed: %w", err)
	}

	outPart := b.Out + ".part"
	if _, err := os.Stat(b.Out); err == nil && !b.ForceDownload {
		fmt.Printf("Skipping the download step: %s already exists (rerun with --force-download to download anyway)\n", b.Out)
		if err := os.Rename(b.Out, outPart); err != nil {
			return fmt.Errorf("failed to rename %s to %s: %w", b.Out, outPart, err)
		}
	} else {
		if err = os.MkdirAll(filepath.Dir(b.Out), 0755); err != nil {
			return fmt.Errorf("failed to make build dir: %w", err)
		}

		fmt.Printf("Downloading %s...\n", labels.SourceURL)
		rc, n := downloader(labels.SourceURL)
		rc = progress(rc, n, os.Stdout)
		rc = verifier(rc, labels.SourceSHA256)
		rc = decompresser(rc, labels.SourceURL)
		if err := save(rc, outPart); err != nil {
			return fmt.Errorf("failed to download %s: %w", labels.SourceURL, err)
		}
	}

	fmt.Printf("Syncing with %s...\n", outPart)
	reader, err := image.TarOut()
	if err != nil {
		return fmt.Errorf("failed to tar podman image: %w", err)
	}
	defer reader.Close()

	fsys, err := aferoguestfs.OpenPartitionFs(outPart, "/dev/"+labels.SourcePartitionsImport)
	if err != nil {
		return fmt.Errorf("failed to open partition: %w", err)
	}

	if b.Verbose {
		err = aferoSyncVerbose(fsys, tar.NewReader(reader), aferoSyncStdout)
	} else {
		err = aferoSyncCompact(fsys, tar.NewReader(reader), aferoSyncStdout)
	}
	if err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	if err := os.Rename(outPart, b.Out); err != nil {
		return fmt.Errorf("failed to rename: %w", err)
	}

	fmt.Println("Build complete.")
	fmt.Println(b.Out)

	return nil
}

type DiskSyncCmd struct {
	Tar            *os.File `xor:"src" required:"" existingfile:"" help:"Sync from a tar archive (cannot be used with --container-image)"`
	ContainerImage string   `xor:"src" required:"" help:"Sync from a container image (cannot be used with --tar)"`
	Disk           string   `arg:"" help:"Path to disk image"`
	Partition      string   `arg:"" default:"sda2" help:"Partition device (default: sda2)"`
	Verbose        bool     `short:"v" help:"Print paths of all synced files"`
}

func (cmd *DiskSyncCmd) Run() error {
	afs, err := aferoguestfs.OpenPartitionFs(cmd.Disk, "/dev/"+cmd.Partition)
	if err != nil {
		return fmt.Errorf("failed to open partition: %w", err)
	}

	var tarReader *tar.Reader
	if cmd.Tar != nil {
		tarReader = tar.NewReader(cmd.Tar)
		defer cmd.Tar.Close()
	} else if cmd.ContainerImage != "" {
		rc, err := podman.Image{Name: cmd.ContainerImage}.TarOut()
		if err != nil {
			return fmt.Errorf("failed to tar container image %s: %w", cmd.ContainerImage, err)
		}
		defer rc.Close()
		tarReader = tar.NewReader(rc)
	}

	if cmd.Verbose {
		err = aferoSyncVerbose(afs, tarReader, os.Stdout)
	} else {
		err = aferoSyncCompact(afs, tarReader, os.Stdout)
	}
	if err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	return nil
}

func aferoSyncVerbose(afs afero.Fs, tarReader *tar.Reader, w io.Writer) error {
	sync := aferosync.New(afs, tarReader)
	for sync.Next() {
		fmt.Fprintln(w, sync.Update())
	}
	fmt.Fprintln(w, sync.Summary())
	return sync.Err()
}

func aferoSyncCompact(afs afero.Fs, tarReader *tar.Reader, w io.Writer) error {
	sync := aferosync.New(afs, tarReader)
	firstUpdate := true
	fmt.Fprint(w, sync.Summary())
	for sync.Next() {
		if firstUpdate {
			fmt.Fprint(w, "\r"+eraseInDisplay)
		} else {
			fmt.Fprintf(w, cursorUp+"\r"+eraseInDisplay, 1)
		}
		fmt.Fprintln(w, sync.Update())
		fmt.Fprint(w, sync.Summary())
		firstUpdate = false
	}
	fmt.Fprintln(w)
	return sync.Err()
}

func removeArchiveExt(name string) string {
	decompressors := []archives.Format{
		archives.Brotli{},
		archives.Bz2{},
		archives.Gz{},
		archives.Lz4{},
		archives.Lzip{},
		archives.MinLZ{},
		archives.Sz{},
		archives.Xz{},
		archives.Zlib{},
		archives.Zstd{},
	}

	ext := filepath.Ext(name)
	for _, d := range decompressors {
		if ext == d.Extension() {
			return name[:len(name)-len(ext)]
		}
	}

	return name
}
