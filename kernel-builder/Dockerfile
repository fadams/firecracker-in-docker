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

# Note that the ubuntu:20.04 default compiler fails to build older kernels
# like the v4.20 kernel used in the firecracker documentation.
# https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md
# This isn't an issue here as the aim is to build a more recent kernel, but
# if an older kernel *is* required simply change base image to ubuntu:18.04
# which works for kernels from v4.20 to (at least) v5.14.
FROM ubuntu:20.04

COPY resources/* /usr/local/bin/

RUN apt-get update && DEBIAN_FRONTEND=noninteractive \
    apt-get install -y --no-install-recommends \
    bc bison build-essential ca-certificates flex \
    libelf-dev libncurses5-dev libssl-dev curl && \
    # Create build dir and builder user so container may also be run
    # without bind-mounting a build directory, if so desired.
    mkdir -p /tmp/src && chmod 777 /tmp/src && \
    useradd -U builder && \
    rm -rf /var/lib/apt/lists/*

USER builder

WORKDIR /tmp/src

ENTRYPOINT ["/usr/local/bin/make-kernel"]

#-------------------------------------------------------------------------------
# 
# To build the image
# docker build -t firecracker-kernel-builder .
#

