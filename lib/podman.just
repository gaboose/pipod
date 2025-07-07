[no-cd]
import path_to_image_file partition_number docker_image_name:
    #!/usr/bin/env bash
    set -e
    image_file=$(realpath {{path_to_image_file}})

    just disks mount $image_file {{partition_number}}
    info=$(just disks mount_info $image_file {{partition_number}})

    sudo tar cf - -C $(echo $info | jq -r '.mountpoints[0]') . | sudo podman import - {{docker_image_name}}

    just disks unmount $image_file {{partition_number}}

[no-cd]
export docker_image_name path_to_image_file partition_number:
    #!/usr/bin/env bash
    set -e
    image_file=$(realpath {{path_to_image_file}})

    just disks mount $image_file {{partition_number}}
    info=$(just disks mount_info $image_file {{partition_number}})

    mnt=$(sudo podman image mount {{docker_image_name}})
    sudo rsync -av --delete ${mnt}/ $(echo $info | jq -r '.mountpoints[0]')
    sudo podman image unmount {{docker_image_name}}

    just disks unmount $image_file {{partition_number}}
