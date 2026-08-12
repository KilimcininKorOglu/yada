# A throwaway Unbound server reachable over ssh, for exercising the CLI
# against something real instead of a mock.
FROM debian:bookworm-slim

# dnsutils gives the container dig, so a test can ask the resolver what it
# actually answers rather than trusting the config file.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        unbound \
        unbound-anchor \
        openssh-server \
        sudo \
        dnsutils \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# systemd does not run in this container, so systemctl is replaced by a shim
# that drives the unbound process directly. That keeps the two systemctl-based
# refresh tiers on their real code paths instead of skipping them.
COPY systemctl-shim.sh /usr/local/bin/systemctl
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 0755 /usr/local/bin/systemctl /usr/local/bin/entrypoint.sh

# The test account matches what the example configuration expects, and gets
# passwordless sudo the same way a real deployment would.
RUN useradd --create-home --shell /bin/bash user01 \
    && install -d -m 0700 -o user01 -g user01 /home/user01/.ssh \
    && printf 'user01 ALL=(ALL) NOPASSWD: ALL\n' > /etc/sudoers.d/user01 \
    && chmod 0440 /etc/sudoers.d/user01 \
    && install -d -m 0755 /run/sshd

COPY unbound.conf /etc/unbound/unbound.conf
COPY local_records.conf /etc/unbound/local_records.conf

# The records file is what the tool rewrites, so the account has to own it.
RUN chown user01:user01 /etc/unbound/local_records.conf \
    && chmod 0644 /etc/unbound/local_records.conf

EXPOSE 22 53/tcp 53/udp

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
