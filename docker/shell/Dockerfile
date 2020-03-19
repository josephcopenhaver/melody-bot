ARG DOCKER_GO_VERSION=1.14.0-buster

FROM golang:${DOCKER_GO_VERSION}

RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y \
    apt-transport-https \
    autossh \
    bash \
    bash-completion \
    build-essential \
    ca-certificates \
    curl \
    dnsutils \
    expect \
    ftp \
    git \
    gnupg2 \
    jq \
    make \
    ncftp \
    net-tools \
    netcat \
    redir \
    rsync \
    software-properties-common \
    tar \
    tmate \
    tmux \
    vim \
    && rm -rf /var/lib/apt/lists/* \
    && true

RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y \
    python \
    python-pip \
    python-wheel \
    && rm -rf /var/lib/apt/lists/* \
    && pip install \
    awscli \
    cfn-lint \
    yq \
    && true

WORKDIR /workspace
