# Auto-generated Dockerfile based on scripts/install.sh
FROM ubuntu:24.04

ARG DAYZ_HOME=/srv/dayz
ARG STEAM_USER=kqkklan
ENV DAYZ_HOME=${DAYZ_HOME}
ENV STEAM_USER=${STEAM_USER}

RUN apt-get update -qq \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
       ca-certificates curl tar gzip rsync jq lib32gcc-s1 util-linux \
    && rm -rf /var/lib/apt/lists/*

# Create dayz user and directories
RUN useradd -m -d ${DAYZ_HOME} -s /bin/bash dayz \
    && mkdir -p ${DAYZ_HOME}/steamcmd ${DAYZ_HOME}/server ${DAYZ_HOME}/backups ${DAYZ_HOME}/workshop ${DAYZ_HOME}/state \
    && chown -R dayz:dayz ${DAYZ_HOME}

# Install SteamCMD as the dayz user
RUN set -eux; \
    cd ${DAYZ_HOME}/steamcmd; \
    su -s /bin/sh -c "curl -sSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz | tar -xz" dayz; \
    su -s /bin/sh -c "${DAYZ_HOME}/steamcmd/steamcmd.sh +quit" dayz

# Fetch dayzctl binary from GitHub releases at build time if possible
ARG VERSION=
ARG ARCH=$(dpkg --print-architecture)
RUN set -eux; \
    OS=linux; \
    if [ -n "${VERSION}" ]; then \
        # strip leading v if present (e.g. v1.2.3 -> 1.2.3)
        VERSION_CLEAN=${VERSION#v}; \
        ASSET="dayzctl_${VERSION_CLEAN}_${OS}_${ARCH}.tar.gz"; \
        DL_URL="https://github.com/dayzctl/dayzctl/releases/download/v${VERSION_CLEAN}/${ASSET}"; \
        TMP_DIR=$(mktemp -d); \
        TMP_FILE=${TMP_DIR}/${ASSET}; \
        curl -fsSL -o ${TMP_FILE} ${DL_URL}; \
        tar -xzf ${TMP_FILE} -C ${TMP_DIR}; \
        install -m 0755 ${TMP_DIR}/dayzctl /usr/local/bin/dayzctl; \
        rm -rf ${TMP_DIR}; \
    else \
        echo "No VERSION provided, expecting dayzctl to be provided by volume or multi-stage build"; \
    fi

# Create default config directory and placeholder config file (can be overridden)
RUN mkdir -p /etc/dayzctl && echo "# volume-mounted config" > /etc/dayzctl/config.yaml

VOLUME ["/srv/dayz"]

WORKDIR /srv/dayz

ENTRYPOINT ["/usr/local/bin/dayzctl"]
CMD ["--help"]
