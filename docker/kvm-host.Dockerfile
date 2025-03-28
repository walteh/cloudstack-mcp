FROM --platform=linux/amd64 ubuntu:22.04

# Install QEMU, KVM, libvirt and other required packages
RUN apt-get update && apt-get install -y \
    qemu-kvm \
    libvirt-daemon-system \
    libvirt-clients \
    bridge-utils \
    virtinst \
    openssh-server \
    python3 \
    python3-libvirt \
    uml-utilities \
    iptables \
    iproute2 \
    dnsmasq \
    procps \
    --no-install-recommends \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Configure libvirt to listen on TCP
RUN sed -i 's/#listen_tls = 0/listen_tls = 0/' /etc/libvirt/libvirtd.conf \
    && sed -i 's/#listen_tcp = 1/listen_tcp = 1/' /etc/libvirt/libvirtd.conf \
    && sed -i 's/#auth_tcp = "sasl"/auth_tcp = "none"/' /etc/libvirt/libvirtd.conf \
    && sed -i 's/#tcp_port = "16509"/tcp_port = "16509"/' /etc/libvirt/libvirtd.conf \
    && sed -i 's/#listen_addr = "192.168.0.1"/listen_addr = "0.0.0.0"/' /etc/libvirt/libvirtd.conf

# Enable and configure SSH
RUN mkdir /var/run/sshd \
    && echo 'root:password' | chpasswd \
    && sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config \
    && sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config

# Create required directories
RUN mkdir -p /var/lib/libvirt/images \
    && mkdir -p /var/cloudstack/primary \
    && mkdir -p /var/cloudstack/secondary \
    && mkdir -p /var/run/libvirt

# Create a storage pool for CloudStack
COPY docker/cloudstack-primary-pool.xml /root/primary-pool.xml

# Entry point script
COPY docker/kvm-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/kvm-entrypoint.sh

# Expose ports for libvirt and SSH
EXPOSE 16509 22

ENTRYPOINT ["/usr/local/bin/kvm-entrypoint.sh"] 