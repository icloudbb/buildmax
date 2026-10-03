FROM golang:1.26-alpine3.23 AS build
WORKDIR /src
# The server is a packaging of internal/testsupport/mockoidc, which is
# stdlib-only, so copying that one package keeps the build free of downloads.
COPY go.mod go.sum ./
COPY internal/testsupport/mockoidc ./internal/testsupport/mockoidc
COPY deployment/smoke/mock-oidc ./deployment/smoke/mock-oidc
RUN CGO_ENABLED=0 go build -o /mock-oidc ./deployment/smoke/mock-oidc

FROM scratch
COPY --from=build /mock-oidc /mock-oidc
EXPOSE 9443
ENTRYPOINT ["/mock-oidc"]
