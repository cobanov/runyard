# The web build is embedded in a static Go binary; the image holds only that binary.
FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/webdist/dist ./webdist/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/runyard-server . && mkdir /out/media

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/runyard-server /runyard-server
# owned by the nonroot user, so an empty named volume mounted here starts writable
COPY --from=server --chown=nonroot:nonroot /out/media /media
ENV MEDIA_DIR=/media ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/runyard-server"]
