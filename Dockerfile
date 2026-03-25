# UI build stage
FROM node:22 AS ui-build

WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Vite outputs to ../console/dist (relative to frontend/)
RUN mkdir -p ../console/dist
RUN npm run build

# Go build stage
FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Copy built UI assets into the embed directory
COPY --from=ui-build /src/console/dist/ console/dist/
RUN CGO_ENABLED=0 make build

# Runtime stage
FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /src/bin/holos-console /bin/holos-console

ENTRYPOINT ["/bin/holos-console"]
