ARG DOCKER_GO_VERSION=1.20.2-bullseye

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
    mandoc \
    unzip \
    python3 \
    python3-pip \
    python3-wheel \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install \
    cfn-lint \
    yq \
    && ( \
        rm -rf /tmp/awscli \
        && mkdir -p /tmp/awscli \
        && curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-$(uname -m).zip" -o "/tmp/awscli/awscliv2.zip" \
        && cd /tmp/awscli \
        && unzip awscliv2.zip \
        && ./aws/install \
        && aws --version \
        && cd /tmp \
        && rm -rf awscli \
        && true \
    ) \
    && true

# setup bash completions
RUN test -f /usr/share/bash-completion/bash_completion \
    && echo 'source /usr/share/bash-completion/bash_completion' >> /root/.bashrc \
    && echo 'test -f $HOME/.bashrc.local && source $HOME/.bashrc.local' >> /root/.bashrc \
    && true

# install mage builder
ENV MAGE_VERSION="v1.15.0"
ENV MAGE_HOME="$GOPATH/src/github.com/magefile/mage"
RUN mkdir -p "$MAGE_HOME"  \
    && cd "$MAGE_HOME" \
    && git init \
    && git remote add origin https://github.com/magefile/mage.git \
    && git fetch origin "$MAGE_VERSION" \
    && git reset --hard FETCH_HEAD \
    && go run bootstrap.go \
    && mage -version

# install runtime required packages
RUN mkdir -p /tmp/ffmpeg \
    && curl -fsSL "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-$(bash -c 'x="$(uname -m)" && if [[ "$x" = "x86_64" ]]; then printf "amd64" ; elif [[ "$x" == "aarch64" ]]; then printf "arm64" ; else printf "%s" "$x" ; fi')-static.tar.xz" -o /tmp/ffmpeg/ffmpeg.tar.xz \
    && (cd /tmp/ffmpeg && tar -xf ffmpeg.tar.xz && mv ffmpeg-*-static/ffmpeg ffmpeg) \
    && mv /tmp/ffmpeg/ffmpeg /bin/ffmpeg \
    && chmod a+x /bin/ffmpeg \
    && rm -rf /tmp/ffmpeg \
    && ffmpeg -version

# # install ffmpeg source files
# RUN export DEBIAN_FRONTEND=noninteractive \
#     && apt-get update \
#     && apt-get install -y \
#     autoconf automake build-essential libass-dev libfreetype6-dev libsdl1.2-dev libtheora-dev libtool libva-dev libvdpau-dev libvorbis-dev libxcb1-dev libxcb-shm0-dev libxcb-xfixes0-dev pkg-config texi2html zlib1g-dev \
#     libavdevice-dev libavfilter-dev libswscale-dev libavcodec-dev libavformat-dev libswresample-dev libavutil-dev \
#     yasm

# specify a default working directory
WORKDIR /workspace
