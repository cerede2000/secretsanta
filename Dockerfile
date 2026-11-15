# syntax=docker/dockerfile:1
# Image multi-étapes : binaire statique (sans CGO), image finale distroless
# sans shell, utilisateur non root. Aucun secret n'est défini ici : ils sont
# fournis à l'exécution par l'environnement.
ARG GO_VERSION=1.27

FROM golang:${GO_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# Architecture cible : fournie par BuildKit (--platform) ; la CI construit
# linux/amd64. Vide (constructeur classique), Go retient celle de l'hôte.
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/secretsanta ./cmd/secretsanta \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/secretsanta /secretsanta
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data \
    LISTEN_ADDR=:8080
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/secretsanta", "healthcheck"]
ENTRYPOINT ["/secretsanta"]
