[no-cd]
mount path_to_image_file partition_number:
    #!/usr/bin/env bash
    set -e
    image_file=$(realpath {{path_to_image_file}})

    output=$(LC_ALL=C udisksctl loop-setup -f $image_file --no-user-interaction)
    echo $output
    device=$(echo "$output" | grep -o '/dev/loop[0-9]*')

    udisksctl mount -b "${device}p{{partition_number}}" --no-user-interaction

[no-cd]
unmount path_to_image_file partition_number:
    #!/usr/bin/env bash
    set -e
    image_file=$(realpath {{path_to_image_file}})

    info=$(just disks mount_info $image_file {{partition_number}})
    udisksctl unmount --no-user-interaction -b $(echo $info | jq -r '.partition_device')
    udisksctl loop-delete --no-user-interaction -b $(echo $info | jq -r '.image_device')

[no-cd]
mount_info path_to_image_file partition_number:
    #!/usr/bin/env bash
    set -e
    image_file=$(realpath {{path_to_image_file}})

    # Get all mounted devices of the given partition number
    devices=$(lsblk -lnpb --json | jq -r '.blockdevices[] | select((.name | startswith("/dev/loop") and endswith("p{{partition_number}}")) and (.mountpoints | length) > 0) | .name')

    # Find the one that is backed by the given image file
    for device in $devices; do
        image_device=${device%"p{{partition_number}}"}
        backing_file=$(LC_ALL=C udisksctl info -b $image_device | grep -Po '^ *BackingFile: *\K.*')

       if [ "$backing_file" = "$image_file" ]; then
            lsblk -lnpb --json | jq --arg partition "$device" --arg image "$image_device" -r '.blockdevices[] | select(.name == $partition) | {"image_device": $image, "partition_device": $partition, "mountpoints": .mountpoints}'
            exit 0
        fi 
    done;

    exit 1