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
	"github.com/gaboose/pipod/internal/wifi"
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
	Spec     string `default:"pipod.toml" help:"Path to pipod.toml" type:"existingfile"`
	Tag      string `short:"t" help:"Tagged name to apply to the built image"`
	Manifest string `help:"Add the images to a manifest list. Creates manifest list if it does not exist"`
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

	if len(spec.Platform) > 1 && b.Manifest == "" {
		return fmt.Errorf("--manifest must be set when building a multiarch image")
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
		if platform.Labels.SourceSHA256 != "" {
			rc = verifier(rc, platform.Labels.SourceSHA256)
		}
		rc = decompresser(rc, platform.Labels.SourceURL)
		if err := save(rc, workingPath+".part"); err != nil {
			return fmt.Errorf("%s: %w", platform.Labels.SourceURL, err)
		}

		if err := os.Rename(workingPath+".part", workingPath); err != nil {
			return fmt.Errorf("failed to rename: %w", err)
		}

		name := b.Tag
		if len(spec.Platform) > 1 && name != "" {
			name += "-" + platformName
			name = strings.ReplaceAll(name, ":", "-")
			name = strings.ReplaceAll(name, "/", "-")
		}

		if name == "" {
			fmt.Printf("Importing...\n")
		} else {
			fmt.Printf("Importing %s...\n", name)
		}
		readCloser := guestfish.TarOut(workingPath, "/dev/"+platform.Labels.GetSourcePartitionsImport())

		importOpts := []podman.ImportOption{
			podman.WithPlatform(platformName),
			podman.WithLabels(spec.Labels),
			podman.WithLabelsToml(platform.Labels),
		}

		if name != "" {
			importOpts = append(importOpts, podman.WithName(name))
		}

		img, err := podman.Import(readCloser, importOpts...)
		if err != nil {
			return fmt.Errorf("failed to import podman image: %w", err)
		}

		images = append(images, img.Name)
	}

	var reference string
	if len(spec.Platform) == 1 {
		reference = images[0]
	} else {
		fmt.Printf("Creating manifest %s...\n", b.Manifest)
		if reference, err = podman.CreateManifest(b.Manifest, images); err != nil {
			return fmt.Errorf("failed to create manifest: %w", err)
		}
	}

	fmt.Println("Build complete.")
	fmt.Println(reference)
	return nil
}

type DiskCmd struct {
	Build DiskBuildCmd `cmd:"" help:"Build a disk image from a Containerfile"`
	Sync  DiskSyncCmd  `cmd:"" help:"Sync a disk image from a tar or a container image"`
	Wifi  DiskWifiCmd  `cmd:"" help:"Set up a wifi connection"`
}

type DiskBuildCmd struct {
	Out           string `short:"o" help:"File to write to" default:"build/out.img"`
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

	var labels PipodLabels
	if err := image.UnmarshalLabelsToml(&labels); err != nil {
		return fmt.Errorf("failed to get image labels: %w", err)
	}

	if err := labels.validate(); err != nil {
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
		if labels.SourceSHA256 != "" {
			rc = verifier(rc, labels.SourceSHA256)
		}
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

	fsys, err := aferoguestfs.OpenPartitionFs(outPart, "/dev/"+labels.GetSourcePartitionsImport())
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
	Disk           string   `arg:"" help:"Path to disk image"`
	Partition      string   `default:"sda2" help:"Partition device (default: sda2)"`
	Tar            *os.File `xor:"src" required:"" existingfile:"" help:"Sync from a tar archive (cannot be used with --container-image)"`
	ContainerImage string   `xor:"src" required:"" help:"Sync from a container image (cannot be used with --tar)"`
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

type DiskWifiCmd struct {
	Disk          string `arg:"" help:"Path to disk image"`
	Partition     string `default:"sda2" help:"Partition device (default: sda2)"`
	SSID          string `required:"" help:"SSID fot wifi network to connect to"`
	Password      string `xor:"P" required:"" help:"Password of the SSID network (cannot be used with --password-stdin)"`
	PasswordStdin bool   `xor:"P" required:"" help:"Read password from stdin (cannot be used with --password)"`
}

func (cmd *DiskWifiCmd) Run() error {
	afs, err := aferoguestfs.OpenPartitionFs(cmd.Disk, "/dev/"+cmd.Partition)
	if err != nil {
		return fmt.Errorf("failed to open partition: %w", err)
	}
	defer afs.Close()

	if cmd.PasswordStdin {
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read form stdin: %w", err)
		}
		cmd.Password = strings.TrimSpace(string(buf))
	}

	nm, err := wifi.NewNetworkManager(afs)
	if os.IsNotExist(err) {
		return fmt.Errorf("network manager not found")
	} else if err != nil {
		return fmt.Errorf("failed to create NetworkManager: %w", err)
	}
	fmt.Println("NetworkManager detected")

	addedPaths, err := nm.AddConnection(cmd.SSID, cmd.Password)
	if err != nil {
		return fmt.Errorf("failed to add connection profile: %w", err)
	}

	for _, path := range addedPaths {
		fmt.Printf("added %s\n", path)
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
