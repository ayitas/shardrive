# syntax=docker/dockerfile:1

FROM oven/bun:1

ARG PROTON_SDK_REPOSITORY=https://github.com/ProtonDriveApps/sdk.git
ARG PROTON_SDK_COMMIT=c8d03244938a6b4d107c755df8904d7d971ed1c2

WORKDIR /app

RUN apt-get update \
    && apt-get install --no-install-recommends -y ca-certificates git \
    && rm -rf /var/lib/apt/lists/* \
    && git clone --no-checkout "${PROTON_SDK_REPOSITORY}" /opt/proton-sdk \
    && git -C /opt/proton-sdk checkout --detach "${PROTON_SDK_COMMIT}" \
    && test "$(git -C /opt/proton-sdk rev-parse HEAD)" = "${PROTON_SDK_COMMIT}" \
    && cd /opt/proton-sdk/cli \
    && bun install --frozen-lockfile \
    && cd /opt/proton-sdk/client/js \
    && bun install \
    && cd /opt/proton-sdk/incubating/account/js \
    && bun install

COPY apps/proton-adapter/package.json apps/proton-adapter/package-lock.json ./
RUN bun install --production

COPY apps/proton-adapter/src ./src
COPY apps/proton-adapter/scripts ./scripts
COPY proto /workspace/proto

ENV PROTON_SDK_SOURCE_DIR=/opt/proton-sdk \
    PROTON_SDK_EXPECTED_COMMIT=c8d03244938a6b4d107c755df8904d7d971ed1c2 \
    SHARDRIVE_PROTO_PATH=/workspace/proto/storage.proto \
    SHARDRIVE_PROTON_GRPC_ADDRESS=0.0.0.0:50051 \
    SHARDRIVE_PROTON_SESSION_ROOT=/var/lib/shardrive/proton-sessions

VOLUME ["/var/lib/shardrive/proton-sessions"]
EXPOSE 50051

CMD ["bun", "scripts/proton-adapter-server.mjs"]
