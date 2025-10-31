#include <tunables/global>

profile qudata-agent /usr/local/bin/qudata-agent {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  # --- доступ к файлам проекта в рабочей директории ---
  /opt/qudata-agent/state.json rw,
  /opt/qudata-agent/secret.json rw,
  /opt/qudata-agent/lockdown.lock w,

  # --- доступ к хранилищу и точкам монтирования ---
  /var/lib/qudata/storage/** rwk,
  /var/lib/qudata/mounts/** rwk,

  # --- доступ к системным файлам ---
  /etc/machine-id r,
  /var/lib/dbus/machine-id r,
  /proc/sys/kernel/random/boot_id r,
  /proc/cpuinfo r,
  /proc/meminfo r,

  # --- дступ к сокетам и устройствам ---
  /var/run/docker.sock rw,
  # /run/docker/plugins/qudata-authz.sock rw, # ещё не реализовано
  /dev/mapper/* rw,
  /dev/loop* rw,
  /dev/vfio/** rw,

  # --- разрешение на запуск утилит ---
  /usr/bin/truncate ix,
  /usr/sbin/cryptsetup ix,
  /usr/sbin/mkfs.ext4 ix,
  /usr/bin/mount ix,
  /usr/bin/umount ix,
  /usr/bin/shred ix,
  /usr/sbin/iptables ix,
  /usr/bin/lspci ix,

  # --- сетевые правила ---
  network,

  # --- системные возможности ---
  capability sys_admin,
  capability audit_write,
  capability audit_control,

  # --- правила самозащиты ---
  deny /usr/local/bin/qudata-agent w, # запрет на запись в собственный бинарь
  deny ptrace (tracedby), # запрет на отладку
}