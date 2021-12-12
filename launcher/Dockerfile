#
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
# 
#   http://www.apache.org/licenses/LICENSE-2.0
# 
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.
#

FROM ubuntu:20.04

ARG FC_VERSION=v0.25.2

COPY kernel/vmlinux /usr/local/bin/vmlinux
COPY resources/firestarter /usr/local/bin/firestarter

RUN apt-get update && DEBIAN_FRONTEND=noninteractive \
    apt-get install -y --no-install-recommends \
    # e2tools provides utilities for manipulating files in ext4 filesystems
    # in userspace, so we can avoid using mount which would require a much
    # more privileged container than we would like to use.
    e2tools \
    curl ca-certificates iproute2 iptables && \
    # To set up networking to the Firecracker MicroVM we need to establish
    # a tap device in the container, then create a nat or bridge. To create
    # the tap we need CAP_NET_ADMIN and to establish the nat routing we need
    # CAP_NET_ADMIN and CAP_NET_RAW. To do this we create a copy of ip and
    # iptables and use setcap to add the required capabilities to the copies.
    cp /usr/bin/ip /usr/local/bin/ip && \
    setcap cap_net_admin=eip /usr/local/bin/ip && \
    cp /usr/sbin/iptables /usr/local/bin/iptables && \
    setcap cap_net_admin,cap_net_raw=eip /usr/local/bin/iptables && \
    # In addition to needing the required capabilities, iptables fails with:
    # Fatal: can't open lock file /run/xtables.lock: Permission denied
    # To resolve this we create the lock file with mode 0666
    # https://patchwork.ozlabs.org/project/netdev/patch/2ef2150d-7149-cf39-0ea8-78edfc2c8d92@virtuozzo.com/
    # This doesn't reduce security, as it's only a lockfile, and indeed
    # upstream iptables has made the location configurable via the 
    # XTABLES_LOCKFILE env var for exactly the scenario where the user has
    # granted enough capabilities but lacks access to the XT_LOCK_NAME.
    # https://git.netfilter.org/iptables/commit/?id=5105e844c2ff6a3e320bf8cdadc5094d4eaef2b7
    # but unfortunately iptables from ubuntu:20.04 doesn't yet support that.
    touch /run/xtables.lock && chmod 666 /run/xtables.lock && \
    # Fetch firecracker release and install to /usr/local/bin
    curl -sSL https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-x86_64.tgz | tar -xzv -C /usr/local/bin && \
    cd /usr/local/bin && \
    mv release-${FC_VERSION}-x86_64/firecracker-${FC_VERSION}-x86_64 firecracker && \
    rm -rf release-${FC_VERSION}-x86_64 && \
    apt-get clean && \
    apt-get purge -y curl ca-certificates && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["/usr/local/bin/firestarter"]

#-------------------------------------------------------------------------------
# 
# To build the image
# docker build -t firecracker-in-docker .
#
# These packages are useful for debugging, but are not  for operation:
# iputils-ping net-tools
#

