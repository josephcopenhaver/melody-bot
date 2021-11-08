ARG SHELL_VERSION=latest
FROM josephcopenhaver/melody-bot--shell:$SHELL_VERSION

# specify a working default working directory
WORKDIR /workspace

COPY ./ ./

RUN ./scripts/ci-build.sh \
    && true

FROM debian:10.3

# specify a default working directory
WORKDIR /workspace

# install very basic networking utilities
RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y \
    bash \
    ca-certificates \
    curl \
    netcat \
    && true

# install runtime required packages
RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get update \
    && apt-get install -y \
    libopus-dev \
    libopusfile-dev \
    opus-tools \
    ffmpeg

# # install ffmpeg source files
# RUN export DEBIAN_FRONTEND=noninteractive \
#     && apt-get update \
#     && apt-get install -y \
#     autoconf automake build-essential libass-dev libfreetype6-dev libsdl1.2-dev libtheora-dev libtool libva-dev libvdpau-dev libvorbis-dev libxcb1-dev libxcb-shm0-dev libxcb-xfixes0-dev pkg-config texi2html zlib1g-dev \
#     libavdevice-dev libavfilter-dev libswscale-dev libavcodec-dev libavformat-dev libswresample-dev libavutil-dev \
#     yasm


COPY --from=0 /workspace/build/bin/ ./build/bin/

ENV LOG_FORMAT=json

ENTRYPOINT ["./build/bin/melody-bot"]
