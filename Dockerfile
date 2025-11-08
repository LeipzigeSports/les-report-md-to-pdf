FROM golang:1.25.3-trixie AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /usr/local/bin/reportconv .

FROM pandoc/typst:3.8.2-debian

WORKDIR /opt/reportconv

COPY --from=builder /usr/local/bin/reportconv /opt/reportconv/reportconv
COPY resources ./resources

# -r (system user), -M (don't create home directory), -d (specify home directory)
RUN set -ex && useradd -r -M -d /opt/reportconv reportconv && chown -R reportconv /opt/reportconv
USER reportconv

ENV PANDOC_EXECUTABLE=pandoc
ENV APPLICATION_ROOT=/opt/reportconv

ENTRYPOINT ["/opt/reportconv/reportconv"]
