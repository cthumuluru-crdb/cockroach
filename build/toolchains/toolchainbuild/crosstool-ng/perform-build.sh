#!/usr/bin/env bash

# Copyright 2022 The Cockroach Authors.
#
# Use of this software is governed by the CockroachDB Software License
# included in the /LICENSE file.


set -euxo pipefail

apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    apt-transport-https \
    autoconf \
    bison \
    bzip2 \
    ca-certificates \
    cmake \
    curl \
    file \
    flex \
    g++ \
    gawk \
    git \
    gnupg2 \
    gperf \
    help2man \
    libncurses-dev \
    libssl-dev \
    libtool-bin \
    libxml2-dev \
    make \
    patch \
    patchelf \
    python \
    texinfo \
    xz-utils \
    unzip \
    zlib1g \
    zlib1g-dev \
 && apt-get clean

mkdir crosstool-ng \
 && curl -fsSL http://crosstool-ng.org/download/crosstool-ng/crosstool-ng-1.24.0.tar.xz -o crosstool-ng.tar.xz \
 && echo '804ced838ea7fe3fac1e82f0061269de940c82b05d0de672e7d424af98f22d2d crosstool-ng.tar.xz' | sha256sum -c - \
 && tar --strip-components=1 -C crosstool-ng -xJf crosstool-ng.tar.xz \
 && cd crosstool-ng \
 && ./configure --prefix /usr/local/ct-ng \
 && make -j$(nproc) \
 && make install \
 && cd .. \
 && rm -rf crosstool-ng crosstool-ng.tar.xz

mkdir src
build_ctng() {
    mkdir build
    cp /bootstrap/$1.config build/.config
    (cd build && /usr/local/ct-ng/bin/ct-ng build)
    rm -rf build
}
build_ctng x86_64-unknown-linux-gnu
build_ctng x86_64-w64-mingw
build_ctng aarch64-unknown-linux-gnueabi
build_ctng s390x-ibm-linux-gnu
rm -rf src

# Build & install the terminfo lib (incl. in ncurses) for the linux targets (x86, arm and s390x).
# (on BSD or BSD-derived like macOS it's already built-in; on windows we don't need it.)
#
# The patch is needed to work around a bug in Debian mawk, see
# http://lists.gnu.org/archive/html/bug-ncurses/2015-08/msg00008.html
#
# As per the Debian rule file for ncurses, the two configure tests for
# the type of bool and poll(2) are broken when cross-compiling, so we
# need to feed the test results manually to configure via an environment
# variable; see debian/rules on the Debian ncurses source package.
#
# The configure other settings in ncurses.conf are also sourced from the
# Debian source package.
#
mkdir ncurses \
 && curl -fsSL http://ftp.gnu.org/gnu/ncurses/ncurses-6.0.tar.gz -o ncurses.tar.gz \
 && echo 'f551c24b30ce8bfb6e96d9f59b42fbea30fa3a6123384172f9e7284bcf647260 ncurses.tar.gz' | sha256sum -c - \
 && tar --strip-components=1 -C ncurses -xzf ncurses.tar.gz \
 && cd ncurses \
 && patch -p0 <../bootstrap/ncurses.patch
export cf_cv_type_of_bool='unsigned char'
export cf_cv_working_poll=yes
build_ncurses() {
    mkdir build-$1
    (cd build-$1 && \
	 CC=/x-tools/$1/bin/$1-cc CXX=/x-tools/$1/bin/$1-c++ ../configure \
           --prefix=/x-tools/$1/$1/sysroot/usr --host=$1 \
           $(cat /bootstrap/ncurses.conf) \
         && make install.libs)
}
build_ncurses x86_64-unknown-linux-gnu
build_ncurses aarch64-unknown-linux-gnu
build_ncurses s390x-ibm-linux-gnu
cd ..

# Add openssl header files needed by the FIPS build.
mkdir openssl \
 && curl -fsSL https://github.com/openssl/openssl/releases/download/openssl-3.1.2/openssl-3.1.2.tar.gz -o openssl.tar.gz \
 && echo 'a0ce69b8b97ea6a35b96875235aa453b966ba3cba8af2de23657d8b6767d6539 openssl.tar.gz' | sha256sum -c - \
 && tar --strip-components=1 -C openssl -xzf openssl.tar.gz \
 && cd openssl && ./Configure && make && patch -p0 <../bootstrap/crypto.h.patch \
 && cp -r include/openssl /x-tools/x86_64-unknown-linux-gnu/x86_64-unknown-linux-gnu/sysroot/usr/include && cd ..

apt-get purge -y gcc g++ && apt-get autoremove -y

apt-get update \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    clang-10 \
  && update-alternatives --install /usr/bin/clang clang /usr/bin/clang-10 100 \
    --slave /usr/bin/clang++ clang++ /usr/bin/clang++-10

# Bundle artifacts
bundle() {
    filename=/artifacts/$(echo $1 | rev | cut -d/ -f1 | rev).tar.gz
    tar -czf $filename $1
    # Print the sha256 for debugging purposes.
    shasum -a 256 $filename
}
bundle /x-tools/x86_64-unknown-linux-gnu
bundle /x-tools/aarch64-unknown-linux-gnu
bundle /x-tools/s390x-ibm-linux-gnu
bundle /x-tools/x86_64-w64-mingw32
