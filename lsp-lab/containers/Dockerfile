# One image, three targets:
#   devbox - bind-mounted workspace, write & run code
#   runner - code baked in, runs the examples once, stays up for attaching
#   app    - code baked in, long-running HTTP service
# Every target runs sshd (Remote-SSH / JetBrains Gateway) and can be attached
# with VS Code Dev Containers. All lab logic is Go (tools/lab, tools/labinit);
# this file only installs packages and builds.
ARG GO_VERSION=1.26.5

FROM golang:${GO_VERSION}-bookworm AS tools
WORKDIR /build
COPY go.mod ./
COPY tools ./tools
RUN CGO_ENABLED=0 go build -o /out/ ./tools/lab ./tools/labinit

# ---------------------------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS base
RUN apt-get update && apt-get install -y --no-install-recommends \
        openssh-server sudo procps lsof iproute2 psmisc htop git curl less vim-tiny jq \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/sshd && ssh-keygen -A
COPY containers/sshd_config /etc/ssh/sshd_config.d/lab.conf

ARG UID=1000
RUN useradd -m -u ${UID} -s /bin/bash dev \
    && echo 'dev ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/dev \
    && mkdir -p /home/dev/go/pkg/mod /home/dev/.cache/go-build /home/dev/.vscode-server /home/dev/scratch /lab-out \
    && chown -R dev:dev /home/dev /go /lab-out

ENV GOPATH=/home/dev/go \
    GOMODCACHE=/home/dev/go/pkg/mod \
    GOCACHE=/home/dev/.cache/go-build \
    GOTOOLCHAIN=local \
    GOEXPERIMENT=jsonv2 \
    PATH=/home/dev/go/bin:/usr/local/go/bin:$PATH \
    LAB_OUT=/lab-out

USER dev
RUN go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install honnef.co/go/tools/cmd/staticcheck@latest
USER root

COPY --from=tools /out/lab /out/labinit /usr/local/bin/
COPY containers/scratch /opt/scratch-template
RUN ln -s /usr/local/go/bin/go /usr/local/go/bin/gofmt /home/dev/go/bin/dlv /home/dev/go/bin/staticcheck /usr/local/bin/
ENTRYPOINT ["labinit"]
EXPOSE 22

# ---------------------------------------------------------------------------
FROM base AS devbox
WORKDIR /workspace
CMD ["sleep", "infinity"]

# ---------------------------------------------------------------------------
FROM base AS runner
COPY --chown=dev:dev . /src
WORKDIR /src
USER dev
RUN go build ./lsp/shapes ./lsp/store ./lsp/cmd/shapes
USER root
CMD ["lab", "run-examples", "--stay", "/src"]

# ---------------------------------------------------------------------------
FROM base AS app
COPY --chown=dev:dev . /src
WORKDIR /src
USER dev
RUN go build -o /home/dev/go/bin/shapes-server ./lsp/cmd/server
USER root
EXPOSE 8080
CMD ["shapes-server"]
