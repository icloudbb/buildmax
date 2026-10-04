FROM golang:1.26-alpine3.23 AS build
WORKDIR /src
# The server is a packaging of internal/testsupport/mocktelegram, which is
# stdlib-only, so copying that one package keeps the build free of downloads.
COPY go.mod go.sum ./
COPY internal/testsupport/mocktelegram ./internal/testsupport/mocktelegram
COPY deployment/smoke/mock-telegram ./deployment/smoke/mock-telegram
RUN CGO_ENABLED=0 go build -o /mock-telegram ./deployment/smoke/mock-telegram

FROM scratch
COPY --from=build /mock-telegram /mock-telegram
EXPOSE 8080
ENTRYPOINT ["/mock-telegram"]
