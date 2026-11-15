# Imagen mínima de haro: el binario con la web embebida, sobre distroless.
#   docker build -t haro .
#   docker run --rm -v /etc/haproxy:/cfg:ro haro lint /cfg
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /haro ./cmd/haro

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /haro /usr/local/bin/haro
ENTRYPOINT ["/usr/local/bin/haro"]
CMD ["--help"]
