ARG SHELL_VERSION=latest
FROM josephcopenhaver/melody-bot--shell:$SHELL_VERSION

COPY ./ ./

RUN CI=true mage build

FROM gcr.io/distroless/static-debian11

# specify a default working directory
WORKDIR /workspace


COPY --from=0 /workspace/build/bin/ ./build/bin/
COPY --from=0 /bin/ffmpeg /bin/ffmpeg

ENTRYPOINT ["./build/bin/melody-bot"]
