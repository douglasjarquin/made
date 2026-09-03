# Cursor Cloud / local dev base image for this repository.
# Do not COPY the project; Cursor checks out the requested commit itself.
FROM ubuntu:24.04@sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517

ARG GO_VERSION=1.26.5
ARG GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
ARG GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49
ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bubblewrap \
        ca-certificates \
        curl \
        gcc \
        git \
        libc6-dev \
        make \
        sudo \
    && rm -rf /var/lib/apt/lists/* \
    && chmod 4755 "$(command -v bwrap)"

RUN set -e; \
    arch="$TARGETARCH"; \
    if [ -z "$arch" ]; then \
      case "$(uname -m)" in \
        x86_64) arch=amd64 ;; \
        aarch64|arm64) arch=arm64 ;; \
        *) echo "unsupported arch $(uname -m)" >&2; exit 1 ;; \
      esac; \
    fi; \
    case "$arch" in \
      amd64) sha="$GO_SHA256_AMD64"; goarch=amd64 ;; \
      arm64) sha="$GO_SHA256_ARM64"; goarch=arm64 ;; \
      *) echo "unsupported TARGETARCH=$arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz" -o /tmp/go.tar.gz; \
    echo "$sha  /tmp/go.tar.gz" | sha256sum -c -; \
    rm -rf /usr/local/go; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz

RUN id -u ubuntu >/dev/null 2>&1 || useradd -m -s /bin/bash ubuntu \
    && usermod -aG sudo ubuntu \
    && echo "ubuntu ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/ubuntu \
    && mkdir -p /home/ubuntu/.local/bin /home/ubuntu/go/pkg/mod /home/ubuntu/.cache/go-build \
    && chown -R ubuntu:ubuntu /home/ubuntu \
    && printf '%s\n' 'export PATH="/usr/local/go/bin:${HOME}/.local/bin:${PATH}"' > /etc/profile.d/made-path.sh

ENV PATH="/usr/local/go/bin:/home/ubuntu/.local/bin:${PATH}" \
    GOPATH="/home/ubuntu/go" \
    GOMODCACHE="/home/ubuntu/go/pkg/mod" \
    GOCACHE="/home/ubuntu/.cache/go-build" \
    GOTOOLCHAIN=local \
    GOPROXY=https://proxy.golang.org,direct \
    MADE_INSTALL_DIR=/home/ubuntu/.local/bin

USER ubuntu
