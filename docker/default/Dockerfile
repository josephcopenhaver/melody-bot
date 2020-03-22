ARG SHELL_VERSION=latest
FROM josephcopenhaver/discord-bot--shell:$SHELL_VERSION

# specify a working default working directory
WORKDIR /workspace

COPY ./ ./

RUN ./scripts/ci-build.sh \
    && true

FROM debian:10.3

# specify a working default working directory
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

COPY --from=0 /workspace/build/bin/ ./build/bin/

ENV LOG_FORMAT=json

ENTRYPOINT ["./build/bin/discord-bot"]
