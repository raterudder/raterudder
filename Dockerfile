# Build the frontend
FROM node:24-alpine AS vite-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build


# Build the backend
FROM golang:1.27-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY pkg ./pkg
COPY web/embed.go ./web/embed.go
# Copy frontend build to the expected location for embedding
COPY --from=vite-builder /app/web/dist ./web/dist
# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -tags timetzdata -trimpath -ldflags='-s -w' -o raterudder ./cmd/raterudder

# Final image
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=go-builder /app/raterudder /
USER 65532:65532
ENTRYPOINT ["/raterudder"]
