# Build stage
FROM golang:1.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 make build

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /src/bin/holos-console /bin/holos-console

ENTRYPOINT ["/bin/holos-console"]
