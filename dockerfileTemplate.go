package main

import "errors"

func dockerfileTemplate(info RuntimeInfo) (string, error) {
	switch info.Runtime {

	case "node":
		version := "18-alpine"
		if info.Version != "default" {
			version = info.Version
		}
		return `
FROM node:` + version + `
WORKDIR /app
COPY . .
RUN npm install
EXPOSE ${PORT}
CMD ["npm", "start"]
`, nil

	case "python":
		version := "3.11-slim"
		if info.Version != "default" {
			version = info.Version
		}
		return `
FROM python:` + version + `
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE ${PORT}
CMD ["python", "app.py"]
`, nil

	case "static":
		return `
FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE ${PORT}
`, nil

case "go":
	version := "1.23" // default
	if info.Version != "default" {
		version = info.Version
	}
	return `
		# ================================
# Universal Go Dockerfile
# Works for ANY Go project (tiny → production)
# ================================

# Stage 1: Build
FROM golang:` + version + `-alpine AS builder

# Install git and ca-certificates (needed for go mod download and HTTPS in final image)
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum first (enables Docker layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code (handles any project structure automatically)
COPY . .

# Build the binary
# - CGO_ENABLED=0 → fully static binary (no libc dependency)
# - -ldflags="-s -w" → strip debug info → smaller binary
# - -o /main → consistent output name
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o /main .

# Optional: Run tests (uncomment if you want tests to run during build)
# RUN go test -v ./...

# Stage 2: Final minimal image
FROM scratch

# Copy CA certificates (for HTTPS calls)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the static binary
COPY --from=builder /main /main

# Optional: Copy config files, assets, etc. if your app needs them at runtime
# COPY config/ /config/
# COPY templates/ /templates/

# Expose common port (change or override if needed)
EXPOSE 8080

# Non-root user for security (highly recommended in production)
USER 65534:65534

# Run the binary
ENTRYPOINT ["/main"]
`, nil

	default:
		return "", errors.New("no Dockerfile template for runtime: " + info.Runtime)
	}
}


	