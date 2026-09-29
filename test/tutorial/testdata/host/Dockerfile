# The server of `make tutorial-check` (ADR 0030): Ubuntu 24.04 with systemd
# as process 1, Fail2Ban with its sshd jail, OpenSSH, nftables, sudo and
# curl, and the administrator alice, who may use sudo. iproute2 sets up the
# network namespaces of the simulated attacker and peer; python3 serves the
# release to curl. The package lists stay, so that `apt install` works.
# Fail2Ban bans for an hour instead of ten minutes, so that the attacker's
# ban, and so its verdict, outlasts the check on a slow machine.
FROM ubuntu:24.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      systemd systemd-sysv fail2ban nftables openssh-server openssh-client \
      iproute2 sudo curl ca-certificates python3 \
 && systemctl enable ssh \
 && useradd --create-home --shell /bin/bash --groups sudo alice \
 && echo 'alice ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/alice \
 && chmod 0440 /etc/sudoers.d/alice \
 && printf '[DEFAULT]\nbantime = 1h\n' > /etc/fail2ban/jail.d/tutorial-check.conf
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
