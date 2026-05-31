# Build the frontend
FROM node:24-alpine AS vite-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
ARG VITE_JOIN_FORM_URL
RUN VITE_JOIN_FORM_URL=$VITE_JOIN_FORM_URL npm run build


# Build the backend
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY . .
COPY go.mod go.sum ./
RUN go mod download
# Copy frontend build to the expected location for embedding
COPY --from=vite-builder /app/web/dist ./web/dist
# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -tags timetzdata -trimpath -ldflags='-s -w' -o raterudder ./cmd/raterudder

# Final image
FROM gcr.io/distroless/static-debian12
COPY --from=go-builder /app/raterudder /
ENTRYPOINT ["/raterudder"]
