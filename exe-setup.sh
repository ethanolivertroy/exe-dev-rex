#!/bin/bash
# exe-setup.sh — first-boot setup script for exe.dev VMs: join the tailnet via
# the tailscale-wif integration (no auth key), then run Rex as exedev on
# tailnet:7297 so the VM can be added as a Rex host.
#
# exe-rex new passes it to each VM it creates; by hand:
#   ssh exe.dev new --name=foo --tag=rex-host --setup-script=/dev/stdin < exe-setup.sh
#
# Needs: the tailscale-wif integration attached to the VM's tag, and a tailnet
# ACL that lets you reach tag:exe:7297. The integration's metadata says how to
# join: client_id, tags, and ephemeral (true unless set to false).
set -euo pipefail
exec > >(tee -a /tmp/exe-setup.log) 2>&1

VM_USER=exedev
NAME="$(hostname -s)"   # tailnet name = VM name, e.g. scratch

# Stage markers for exe-new's progress bar (it reads them over SSH).
stage() { echo "EXE-STAGE $1 $(date +%s)"; }

# Rex, installed in the background while Tailscale joins. Same apt setup as
# Rex's install.sh (keyring, rex.sources, pin), but apt refreshes only the Rex
# repository instead of every Ubuntu list; Rex's dependencies (libc6, libpam0g,
# passwd) are already on the image. Falls back to install.sh on any failure.
REPO=https://pkgs.superlogical.com/rex
fast_install_rex() {
  local arch keyring=/etc/apt/keyrings/rex-archive-keyring.gpg channel=stable t
  arch=$(dpkg --print-architecture)
  t=$(mktemp -d)
  # Like install.sh: stable if it has been published, else snapshot. The URLs
  # redirect, so follow them and check the content, not just the status.
  curl -fsSL "$REPO/apt/dists/stable/InRelease" 2>/dev/null | grep -q 'BEGIN PGP SIGNED MESSAGE' || channel=snapshot
  sudo install -d -m 0755 /etc/apt/keyrings
  curl -fsSL "$REPO/keys/rex-archive-keyring.gpg" -o "$t/key" && sudo install -m 0644 "$t/key" "$keyring"
  # Same apt config install.sh writes, so `apt upgrade` keeps Rex updated.
  printf 'Types: deb\nURIs: %s/apt\nSuites: %s\nComponents: main\nArchitectures: %s\nSigned-By: %s\n' \
    "$REPO" "$channel" "$arch" "$keyring" | sudo tee /etc/apt/sources.list.d/rex.sources >/dev/null
  printf 'Package: rex\nPin: origin pkgs.superlogical.com\nPin-Priority: 1001\n' | sudo tee /etc/apt/preferences.d/rex >/dev/null

  # Install the .deb directly instead of through apt: apt's own overhead
  # (Ubuntu's package lists, triggers) was ~10s of a ~12s install. Same chain
  # of trust as apt: signed InRelease -> Packages hash -> .deb hash.
  local base="$REPO/apt/dists/$channel" idx="main/binary-$arch/Packages" want got file sha
  curl -fsSL "$base/InRelease" -o "$t/InRelease"
  gpgv --keyring "$keyring" "$t/InRelease" 2>/dev/null || { echo "rex: InRelease signature check failed"; return 1; }
  want=$(awk -v f="$idx" '/^SHA256:/{h=1;next} /^[^ ]/{h=0} h&&$3==f{print $1}' "$t/InRelease")
  curl -fsSL "$base/$idx" -o "$t/Packages"
  got=$(sha256sum "$t/Packages" | cut -d' ' -f1)
  [[ -n "$want" && "$want" == "$got" ]] || { echo "rex: Packages hash mismatch"; return 1; }
  # Newest version of package rex (the index isn't sorted).
  read -r file sha < <(awk '/^Package: /{p=($2=="rex")} p&&/^Version:/{v=$2} p&&/^Filename:/{f=$2} p&&/^SHA256:/{s=$2}
      /^$/{if(p&&f)print v, f, s; p=0;f=""} END{if(p&&f)print v, f, s}' "$t/Packages" |
    sort -V -k1,1 | tail -1 | cut -d' ' -f2-)
  [[ -n "$file" ]] || { echo "rex: no rex package in $channel"; return 1; }
  curl -fsSL "$REPO/apt/$file" -o "$t/rex.deb"
  [[ "$(sha256sum "$t/rex.deb" | cut -d' ' -f1)" == "$sha" ]] || { echo "rex: .deb hash mismatch"; return 1; }
  dpkg-deb -f "$t/rex.deb" Maintainer | grep -qi superlogical || { echo "rex: package isn't Superlogical's"; return 1; }
  sudo env DEBIAN_FRONTEND=noninteractive dpkg -i --force-confdef --force-confold "$t/rex.deb" >/dev/null
  rm -rf "$t"
  dpkg-query -W -f '${Maintainer}' rex 2>/dev/null | grep -qi superlogical
}
install_rex() {
  dpkg-query -W -f '${Maintainer}' rex 2>/dev/null | grep -qi superlogical && return 0
  fast_install_rex && { echo "rex: fast install ok"; return 0; }
  echo "rex: fast install failed; using install.sh"
  curl -fsSL "$REPO/install.sh" | sh
}
stage rex-install-bg
install_rex &
REX_PID=$!

# 1. Tailscale, enrolled with a 5-minute exe.dev identity token. The join lives
# in its own script so a timer can rerun it: Tailscale removes an ephemeral
# node after 30-60 minutes offline (a paused VM, say), and the VM then rejoins
# the same way it joined first, with a fresh token, under the same name.
stage tailscale
command -v tailscale >/dev/null || curl -fsSL https://tailscale.com/install.sh | sh
sudo systemctl enable --now tailscaled
sudo tee /usr/local/sbin/exe-rex-tailscale-join >/dev/null <<'EOF'
#!/bin/bash
# Join the tailnet with an exe.dev identity token (the tailscale-wif integration).
# --if-needed: only if Tailscale removed this node. Its tailscaled then still
# says Running; it notices on its next reconnect, when the control server hands
# it an interactive login URL. Three minutes offline forces that reconnect.
set -euo pipefail
if [[ "${1:-}" == --if-needed ]]; then
  s=$(tailscale status --json 2>/dev/null || true)
  state=$(jq -r '.BackendState // empty' <<<"$s" 2>/dev/null || true)
  auth=$(jq -r '.AuthURL // empty' <<<"$s" 2>/dev/null || true)
  online=$(jq -r '.Self.Online // false' <<<"$s" 2>/dev/null || true)
  f=/run/exe-rex-offline
  if [[ "$state" != NeedsLogin && -z "$auth" ]]; then
    if [[ "$state" == Running && "$online" == false ]]; then
      n=$(( $(cat "$f" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$f"
      if (( n >= 3 )); then rm -f "$f"; echo "offline 3 checks; restarting tailscaled"; systemctl restart tailscaled; fi
    else
      rm -f "$f"
    fi
    exit 0
  fi
  rm -f "$f"
  echo "tailscale removed this node ($state); rejoining"
  tailscale logout >/dev/null 2>&1 || true  # drop the dead identity
fi
W=https://tailscale-wif.int.exe.xyz
META=$(curl -fsS "$W/metadata")
CID=$(jq -er .client_id <<<"$META")
TAGS=$(jq -r '.tags // "tag:exe"' <<<"$META")
EPH=$(jq -r '.ephemeral // "true"' <<<"$META"); [[ "$EPH" == false ]] || EPH=true
TOKEN=$(curl -fsS "$W/token" | jq -er .token)
tailscale up --hostname="$(hostname -s)" --advertise-tags="$TAGS" \
  --client-id="$CID?ephemeral=$EPH&preauthorized=true" --id-token="$TOKEN"
unset TOKEN
# exe-rex rm reads this: an ephemeral node is removed by logging out.
install -d /etc/exe-rex && echo "$EPH" > /etc/exe-rex/tailscale-ephemeral
# A rejoined node has a new tailnet IP, and Rex can't rebind its listener, so
# restart it. That ends programs running in its sessions (layouts survive).
if [[ "${1:-}" == --if-needed ]] && systemctl --user -M exedev@ is-enabled rex.service >/dev/null 2>&1; then
  echo "restarting Rex for the new tailnet address"
  systemctl --user -M exedev@ restart rex.service
fi
EOF
sudo chmod 755 /usr/local/sbin/exe-rex-tailscale-join
tailscale status >/dev/null 2>&1 || sudo /usr/local/sbin/exe-rex-tailscale-join
if [[ "$(cat /etc/exe-rex/tailscale-ephemeral 2>/dev/null)" == true ]]; then
  sudo tee /etc/systemd/system/exe-rex-tailscale-rejoin.service >/dev/null <<'EOF'
[Unit]
Description=Rejoin the tailnet if Tailscale removed this ephemeral node
After=tailscaled.service network-online.target
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/exe-rex-tailscale-join --if-needed
EOF
  sudo tee /etc/systemd/system/exe-rex-tailscale-rejoin.timer >/dev/null <<'EOF'
[Unit]
Description=Check every minute that this VM is still on the tailnet
[Timer]
OnBootSec=20s
OnUnitActiveSec=60s
[Install]
WantedBy=timers.target
EOF
  sudo systemctl daemon-reload
  sudo systemctl enable --now exe-rex-tailscale-rejoin.timer
fi

# 2. Rex (wait for the background install)
stage rex-install
wait "$REX_PID"

# 3. Rex as a lingering user service of exedev.
stage rex-start
sudo loginctl enable-linger "$VM_USER"
UNIT_DIR="/home/$VM_USER/.config/systemd/user"
sudo -u "$VM_USER" mkdir -p "$UNIT_DIR"
# A tagged node has no user owner, so Rex's default "node owner only" check
# would refuse you; admit identified callers and let the tailnet ACL decide
# who reaches :7297.
sudo -u "$VM_USER" tee "$UNIT_DIR/rex.service" >/dev/null <<'EOF'
[Unit]
Description=Rex server
After=network-online.target

[Service]
ExecStart=/usr/bin/rex server run --extra-listen tailnet://:7297 --tailnet-allow-identified
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
EOF
UID_N="$(id -u "$VM_USER")"
sudo -u "$VM_USER" XDG_RUNTIME_DIR="/run/user/$UID_N" systemctl --user daemon-reload
sudo -u "$VM_USER" XDG_RUNTIME_DIR="/run/user/$UID_N" systemctl --user enable --now rex.service

stage setup-done
echo "exe-setup done: add on the Mac with  rex hosts add \"$NAME - exe.dev\" $NAME.<tailnet>.ts.net"
