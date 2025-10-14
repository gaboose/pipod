package main

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	aferoguestfs "github.com/gaboose/afero-guestfs"
	"github.com/gaboose/aferosync"
	"github.com/gaboose/pipod/internal/guestfish"
	"github.com/gaboose/pipod/internal/podman"
	"github.com/mholt/archives"
	"github.com/pelletier/go-toml/v2"
)

const (
	BUILD_DIR = "build"
	TEMP_DIR  = "tmp"
)

type ContainerCmd struct {
	Build ContainerBuildCmd `cmd:"" help:"Build a container image"`
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
	Build DiskBuildCmd `cmd:"" help:"Build a disk image"`
}

type DiskBuildCmd struct {
	Platform string `default:"linux/arm64" help:"set the OS/ARCH[/VARIANT] of the image to the provided value"`
}

func (b *DiskBuildCmd) Run(kctx *kong.Context) error {
	image, err := podman.Build(b.Platform)
	if err != nil {
		return fmt.Errorf("failed to build podman image: %w", err)
	}

	var labels podman.PipodLabels
	if err := image.UnmarshalLabels(&labels); err != nil {
		return fmt.Errorf("failed to get image labels: %w", err)
	}

	if err := labels.Validate(); err != nil {
		return fmt.Errorf("labels validation failed: %w", err)
	}

	if err = os.Mkdir(BUILD_DIR, 0755); err != nil {
		return fmt.Errorf("failed to make build dir: %w", err)
	}

	dest := removeArchiveExt(filepath.Join(BUILD_DIR, filepath.Base(labels.SourceURL))) + ".part"
	rc, n := downloader(labels.SourceURL)
	rc = progress(rc, n, os.Stdout)
	rc = verifier(rc, labels.SourceSHA256)
	rc = decompresser(rc, labels.SourceURL)
	if err := save(rc, dest); err != nil {
		return fmt.Errorf("failed to download %s: %w", labels.SourceURL, err)
	}

	reader, err := image.TarOut()
	if err != nil {
		return fmt.Errorf("failed to tar podman image: %w", err)
	}
	defer reader.Close()

	fsys, err := aferoguestfs.OpenPartitionFs(dest, "/dev/"+labels.SourcePartitionsImport)
	if err != nil {
		return fmt.Errorf("failed to open partition: %w", err)
	}

	sync := aferosync.New(fsys, tar.NewReader(reader))
	for sync.Next() {
		fmt.Println(sync.Update())
	}
	if err := sync.Err(); err != nil {
		return fmt.Errorf("failed to sync partition: %w", err)
	}

	if len(labels.TargetFilename) > 0 {
		if err := os.Rename(dest, filepath.Join(BUILD_DIR, labels.TargetFilename)); err != nil {
			return fmt.Errorf("failed to rename: %w", err)
		}
	}

	return nil
}

type SyncCmd struct {
	Tar       *os.File `arg:"" existingfile:"" help:"Tar archive of an image"`
	Disk      string   `arg:"" help:"Path to disk file"`
	Partition string   `arg:"" help:"Partition device"`
}

func (cmd *SyncCmd) Run() error {
	defer cmd.Tar.Close()

	fmt.Println("Launching...")

	afs, err := aferoguestfs.OpenPartitionFs(cmd.Disk, cmd.Partition)
	if err != nil {
		return fmt.Errorf("failed to open partition: %w", err)
	}

	fmt.Println("Syncing...")

	sync := aferosync.New(afs, tar.NewReader(cmd.Tar))
	for sync.Next() {
		fmt.Println(sync.Update())
	}
	if err := sync.Err(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	return nil
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
