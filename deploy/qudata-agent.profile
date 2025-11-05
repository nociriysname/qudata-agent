#include <tunables/global>

profile qudata-agent /usr/local/bin/qudata-agent {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  # --- Доступ к файлам проекта в рабочей директории ---
  /opt/qudata-agent/state.json rw,
  /opt/qudata-agent/secret.json rw,
  /opt/qudata-agent/lockdown.lock w,

  # --- Доступ к хранилищу и точкам монтирования ---
  /var/lib/qudata/storage/** rwk,
  /var/lib/qudata/mounts/** rwk,

  # --- Доступ к системным файлам ---
  /etc/machine-id r,
  /var/lib/dbus/machine-id r,
  /proc/sys/kernel/random/boot_id r,
  /proc/cpuinfo r,
  /proc/meminfo r,
  # Доступ к /sys для управления драйверами GPU
  /sys/bus/pci/devices/** r,
  /sys/bus/pci/drivers/** rw,

  # --- Доступ к сокетам и устройствам ---
  /var/run/docker.sock rw,
  # Раскомментируем, так как мы это реализовали
  /run/docker/plugins/qudata-authz.sock rwk,
  /dev/mapper/* rw,
  /dev/loop* rw,
  /dev/vfio/** rw,

  # --- Разрешение на запуск утилит ---
  /usr/bin/truncate ix,
  /usr/sbin/cryptsetup ix,
  /usr/sbin/mkfs.ext4 ix,
  /usr/bin/mount ix,
  /usr/bin/umount ix,
  /usr/bin/shred ix,
  /usr/sbin/iptables ix,
  /usr/bin/lspci ix,
  /usr/bin/tee ix,
  /usr/bin/nvidia-smi ix,
  /usr/bin/pgrep ix,

  # --- Сетевые правила ---
  network,

  # --- Системные возможности ---
  capability sys_admin,
  capability audit_write,
  capability audit_control,

  # --- Правила самозащиты ---
  deny /usr/local/bin/qudata-agent w, # Запрет на запись в собственный бинарник
  deny ptrace (tracedby), # Запрет на отладку

  # Разрешаем бинарнику запускать свою собственную копию (для watchdog'а).
  /usr/local/bin/qudata-agent ix,
}