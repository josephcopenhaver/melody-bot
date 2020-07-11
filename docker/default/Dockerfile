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
    python3-pip \
    opus-tools \
    ffmpeg \
    && pip3 install ffmpeg-normalize==1.19.1


COPY --from=0 /workspace/build/bin/ ./build/bin/

ENV LOG_FORMAT=json

ENTRYPOINT ["./build/bin/melody-bot"]
