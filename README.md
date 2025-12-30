# Pipod

Containerfile-based Raspberry Pi image builder inspired by [pidock](https://github.com/eringr/pidock).

## Installation

Pipod depends on your system having [Podman](https://podman.io), [libguestfs](https://libguestfs.org) and [qemu-user-static](https://www.qemu.org) installed.

### Arch Linux

```sh
sudo pacman -S podman libguestfs qemu-user-static
```

and then

```sh
go install github.com/gaboose/pipod
```

## How to Use

### Building a disk image

Once installed, building a new image is only two steps:

1. Write or find a Containerfile that inherits from a [pipod image](#pipod-image). For example:

```Dockerfile
FROM ghcr.io/gaboose/raspios
RUN apt update && apt install -y snapserver && rm -rf /var/lib/apt/lists/*
```

Note: See the [pipod-images](https://github.com/gaboose/pipod-images) repo for more examples.

2. Build it with pipod (no sudo required).

```
pipod disk build -o disk.img
```

This will build the Containerfile, download a raspios raw disk image and overwrite its sda2 partition with the container image filesystem.

---

You can do a lot with this setup but there are limits. For example, containers don't run their own `systemd` so any command that communicates with a service on systemd won't work.

That means you won't be able to set up a wifi connection with a `RUN nmcli ...` instruction. Fortunately, `pipod` can set it up for you by working the raw disk image directly. No mounting, no sudo, just libguestfs under the hood.

```
cat password.txt | pipod disk wifi disk.img --ssid <ssid> --password-stdin
```

Note: This subcommand currently only supports NetworkManager but can easily be extended.

### Building a pipod image

If you don't want to use `ghcr.io/gaboose/raspios`, building your own pipod image is pretty easy too.

1. Create a [build spec](#build-spec). For example:

```toml
# pipod.toml
[platform.'linux/arm64'.labels]
"com.github.gaboose.pipod.source.url" = "https://downloads.raspberrypi.com/raspios_lite_arm64/images/raspios_lite_arm64-2025-10-02/2025-10-01-raspios-trixie-arm64-lite.img.xz"
```

2. Build it with pipod.

```
pipod container build -t mypipodimage
```

This will take an arm64 disk image, import its `sda2` partition device into a podman image and tag it `mypipodimage`.

# Reference

## Useful Commands

### Setup Wifi Connection

```
cat password.txt | pipod disk wifi disk.img --ssid <ssid> --password-stdin
```

### Set User Password

```
virt-customize -a <file> --password '<username>:password:<password>'
```

Replace `<file>` with the disk or partition file, e.g. `/dev/sda`, `/dev/sda2` `./out.img`.

## Pipod Image

A pipod image is a container image with [required labels](#labels) set. Any container image whose parent is a pipod image is also a pipod image.

If you're looking for what to put in your `FROM` instruction, you can pick an image from the [packages section](https://github.com/gaboose?tab=packages&repo_name=pipod) or [build your own](#building-a-pipod-image).

## Build Spec

A build spec is a TOML file (`pipod.toml` by default) used for building a pipod image.

Every key-value pair in a build spec is a container label and is applied to the built image. Sections in the build spec allows to differentiate labels by platform.

```toml
[labels]
## labels here are applied to images of all platforms
# "key" = "value"

[platform.'linux/arm64'.labels]
## labels here are applied to the `linux/arm64` platform image

[platform.'linux/armhf'.labels]
## labels here are applied to the `linux/armhf` platform image
```

Some labels starting with `com.github.gaboose.pipod` are special and determine how the image is built. A valid build spec contains at least one platform section with a `com.github.gaboose.pipod.source.url` label. See [labels](#labels) for more options.

See the [images](images) directory for examples.

## Labels

| Name                                              | Required | Default | Description                                                       |
| ------------------------------------------------- | -------- | ------- | ----------------------------------------------------------------- |
| com.github.gaboose.pipod.source.url               | Y        | -       | Link to the disk image from which this container was created.     |
| com.github.gaboose.pipod.source.sha256            | N        | -       | The SHA256 hash to verify the downloaded source image against.    |
| com.github.gaboose.pipod.source.partitions.import | N        | sda2    | The partition device from which this container image was created. |

## Alternatives

- [pidock](https://github.com/eringr/pidock) - Create raspberry pi disk images with a Dockerfile.
- [pi-gen](https://github.com/RPi-Distro/pi-gen) - Tool used to create Raspberry Pi OS images, and custom images based on Raspberry Pi OS, which was in turn derived from the Raspbian project.
- [Yocto](https://www.yoctoproject.org/), [Raspbery Pi BSP](https://git.yoctoproject.org/meta-raspberrypi/about/) - Professional build system for custom Linux images with a Raspberry Pi board support package.
- [Buildroot](https://buildroot.org/) - Buildroot is a simple, efficient and easy-to-use tool to generate embedded Linux systems through cross-compilation.

## Resources

- [Building Raspberry Pi Disk Images with Docker: a case study in software automation](https://www.boulderes.com/resource-library/building-raspberry-pi-disk-images-with-docker-a-case-study-in-software-automation)
- [Making a more resilient file system](https://pip.raspberrypi.com/categories/685-whitepapers-app-notes/documents/RP-003610-WP/Making-a-more-resilient-file-system.pdf)
