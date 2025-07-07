secrets_dir := "secrets"
chpasswd_file := "chpasswd.txt"
nm_file := "my_wifi.nmconnection"
connman_file := "my_wifi.config"

[no-cd]
password password:
    @mkdir -p {{secrets_dir}}
    @printf "pi:{{password}}" > "{{secrets_dir}}/{{chpasswd_file}}"
    @echo "Written to $(realpath {{secrets_dir}}/{{chpasswd_file}})"

[no-cd]
nm ssid password:
    #!/usr/bin/env bash
    mkdir -p {{secrets_dir}}

    cat > "{{secrets_dir}}/{{nm_file}}" << EOF
    [connection]
    id={{ssid}}
    uuid=`uuidgen`
    type=wifi
    autoconnect=true

    [wifi]
    mode=infrastructure
    ssid={{ssid}}

    [wifi-security]
    key-mgmt=wpa-psk
    psk={{password}}

    [ipv4]
    method=auto

    [ipv6]
    addr-gen-mode=default
    method=auto
    EOF
    echo "Written to $(realpath {{secrets_dir}}/{{nm_file}})"

[no-cd]
connman ssid password:
    #!/usr/bin/env bash
    mkdir -p {{secrets_dir}}

    cat > "{{secrets_dir}}/{{connman_file}}" << EOF
    [service_my_wifi]
    Type = wifi
    Name = {{ssid}}
    Passphrase = {{password}}
    IPv4 = dhcp
    EOF
    echo "Written to $(realpath {{secrets_dir}}/{{connman_file}})"
