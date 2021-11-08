ARG DOCKER_GO_VERSION=1.17.2-buster

FROM golang:${DOCKER_GO_VERSION}

# setup locales for tools that require them ( tmate specifically, probably more )
RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y locales \
    && sed -i -e 's/# en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen \
    && locale-gen

# ensure utf8 locale env variables are set
ENV LANG=en_US.UTF-8 \
    LANGUAGE=en_US:en \
    LC_ALL=en_US.UTF-8

# install system packages
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

# install python packages
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

# setup bash completions
RUN test -f /usr/share/bash-completion/bash_completion \
    && echo 'source /usr/share/bash-completion/bash_completion' >> /root/.bashrc \
    && echo 'test -f $HOME/.bashrc.local && source $HOME/.bashrc.local' >> /root/.bashrc \
    && true

# install runtime required packages
RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y \
    libopus-dev \
    libopusfile-dev  \
    opus-tools \
    ffmpeg

# # install ffmpeg source files
# RUN export DEBIAN_FRONTEND=noninteractive \
#     && apt-get update \
#     && apt-get install -y \
#     autoconf automake build-essential libass-dev libfreetype6-dev libsdl1.2-dev libtheora-dev libtool libva-dev libvdpau-dev libvorbis-dev libxcb1-dev libxcb-shm0-dev libxcb-xfixes0-dev pkg-config texi2html zlib1g-dev \
#     libavdevice-dev libavfilter-dev libswscale-dev libavcodec-dev libavformat-dev libswresample-dev libavutil-dev \
#     yasm

# specify a default working directory
WORKDIR /workspace
